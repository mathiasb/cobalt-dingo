// Package main is the cobalt-dingo server entry point.
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/mathiasb/cobalt-dingo/internal/adapter/fake"
	"github.com/mathiasb/cobalt-dingo/internal/adapter/file"
	adapterfortnox "github.com/mathiasb/cobalt-dingo/internal/adapter/fortnox"
	"github.com/mathiasb/cobalt-dingo/internal/adapter/pisp"
	"github.com/mathiasb/cobalt-dingo/internal/adapter/postgres"
	"github.com/mathiasb/cobalt-dingo/internal/auth"
	"github.com/mathiasb/cobalt-dingo/internal/config"
	"github.com/mathiasb/cobalt-dingo/internal/crypto"
	"github.com/mathiasb/cobalt-dingo/internal/domain"
	"github.com/mathiasb/cobalt-dingo/internal/ui"
)

// supplierCacheTTL is how long IBAN/BIC lookups are cached per supplier.
const supplierCacheTTL = 5 * time.Minute

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	// Fortnox is optional: when FORTNOX_MODE is unset we run in dev mode with
	// fake adapters. When set, the mode is strict — config loading rejects
	// missing credentials so we fail fast on misconfiguration.
	var (
		cfg            config.Fortnox
		fortnoxEnabled bool
	)
	if os.Getenv("FORTNOX_MODE") != "" {
		c, err := config.Load()
		if err != nil {
			log.Error("Fortnox config error", "err", err)
			os.Exit(1)
		}
		cfg = c
		fortnoxEnabled = true
	}

	debtor := config.LoadDebtor()
	appCfg := config.LoadApp()

	if fortnoxEnabled {
		log.Info("cobalt-dingo starting", "port", port, "fortnox_mode", cfg.Mode)
	} else {
		log.Info("cobalt-dingo starting (dev mode — Fortnox unconfigured)", "port", port)
	}

	var (
		invoiceSource domain.InvoiceSource
		enricher      domain.SupplierEnricher
		batchSvc      *domain.BatchService
		erpWriter     domain.ERPWriter
		tokenStore    domain.TokenStore
	)

	// Wire postgres adapters when DATABASE_URL is set.
	var pgStore *postgres.Store
	// Encryption key for everything stored at rest: tenant-supplied Fortnox
	// client secrets (ADR-0005) and the OAuth tokens themselves (#85).
	//
	// Built before the stores because they require it. With a database
	// configured this key is NOT optional: without it tokens cannot be stored
	// at all, and storing them unencrypted in columns named _sealed would be
	// undetectable by inspection.
	var secretCipher *crypto.Cipher
	if raw := os.Getenv("FORTNOX_INTEGRATION_KEY"); raw != "" {
		k, err := crypto.NewCipher(raw)
		if err != nil {
			log.Error("FORTNOX_INTEGRATION_KEY is set but unusable", "err", err)
			os.Exit(1)
		}
		secretCipher = k
	}
	if appCfg.DatabaseURL != "" && secretCipher == nil {
		log.Error("DATABASE_URL is set but FORTNOX_INTEGRATION_KEY is not: Fortnox tokens are encrypted at rest (#85) and cannot be stored without it")
		os.Exit(1)
	}

	var batchRepo domain.BatchRepository
	var tenantRepo domain.TenantRepository
	var ownerDirectory auth.OwnerDirectory
	if appCfg.DatabaseURL != "" {
		var dbErr error
		pgStore, dbErr = postgres.NewStore(appCfg.DatabaseURL)
		if dbErr != nil {
			log.Error("postgres connect failed", "err", dbErr)
			os.Exit(1)
		}
		batchRepo = postgres.NewBatchRepo(pgStore)
		tenantRepo = postgres.NewTenantRepo(pgStore)
		tokenStore = postgres.NewTokenStore(pgStore, secretCipher)
		ownerDirectory = postgres.NewUserDirectory(pgStore, config.LoadOIDC().IssuerURL)
		log.Info("postgres connected")
	}

	pispSubmitter := pisp.NewStub(log)

	if !fortnoxEnabled {
		invoiceSource = fake.InvoiceSource{}
		enricher = fake.SupplierEnricher{}
	} else {
		if tokenStore == nil {
			tokenStore = file.NewTokenStore(cfg.Mode.TokenFile())
		}
		connector := adapterfortnox.NewConnector(cfg, tokenStore, log)
		invoiceSource = connector
		enricher = adapterfortnox.NewCachingEnricher(connector, supplierCacheTTL)
		erpWriter = adapterfortnox.NewERPWriter(connector)
	}

	if batchRepo != nil && tenantRepo != nil {
		batchSvc = domain.NewBatchService(batchRepo, tenantRepo, pispSubmitter, erpWriter)
	}

	// Session manager — always initialised so the ui.Server can read sessions.
	sessions := auth.NewSessionManager(config.LoadSessionSecret())

	// OIDC login — optional; skipped when OIDC_ISSUER_URL is not set.
	oidcCfg := config.LoadOIDC()
	var oidcHandler *auth.OIDCHandler
	if oidcCfg.IsEnabled() {
		defaultMode := config.ModeSandbox
		if fortnoxEnabled {
			defaultMode = cfg.Mode
		}
		if ownerDirectory == nil {
			// ADR-0003: credentials are keyed by an internal user ID, which
			// lives in postgres. Serving OIDC login without it would issue
			// sessions with no credential owner — a login that reports success
			// and then fails at every credential lookup. Refuse instead.
			log.Error("OIDC is enabled but there is no database: credentials are keyed by an internal user ID (ADR-0003), which requires DATABASE_URL")
			os.Exit(1)
		}
		var err error
		oidcHandler, err = auth.NewOIDCHandler(context.Background(), oidcCfg, sessions, defaultMode, ownerDirectory, log)
		if err != nil {
			// Deliberately NOT a downgrade to unauthenticated serving. See
			// secureHandler in wiring.go, which turns this nil into a refusal.
			log.Error("OIDC setup failed", "err", err, "issuer", oidcCfg.IssuerURL)
		} else {
			log.Info("OIDC enabled", "issuer", oidcCfg.IssuerURL)
		}
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))

	// Auth routes — always registered so the middleware redirect target exists.
	if oidcHandler != nil {
		mux.HandleFunc("GET /auth/login", oidcHandler.LoginHandler)
		mux.HandleFunc("GET /auth/callback", oidcHandler.CallbackHandler)
		mux.HandleFunc("GET /auth/logout", oidcHandler.LogoutHandler)
	}

	// Fortnox web-based OAuth connect (per user, per mode, per company).
	// Loaded from all configured modes so users can connect sandbox + production,
	// and sessions is passed so switching the active company can re-issue the
	// session cookie.
	if pgStore != nil {
		modes, incomplete := config.LoadAllModes()
		for _, reason := range incomplete {
			// Loud, because the alternative is a mode that quietly does not
			// appear and a user wondering why.
			log.Warn("fortnox mode not offered", "reason", reason)
		}
		// Per-owner Fortnox integrations (ADR-0005). The cipher is already
		// built and required above, so reaching here means it exists.
		integrationStore := domain.IntegrationStore(postgres.NewIntegrationRepo(pgStore))
		log.Info("per-owner fortnox integrations enabled")

		connector := ui.NewFortnoxConnector(modes, tokenStore, tenantRepo, sessions, integrationStore, secretCipher, log)
		connector.RegisterRoutes(mux)
		log.Info("fortnox connect routes registered")
	}

	srv := ui.NewServer(debtor, invoiceSource, enricher, batchSvc, sessions, log)
	srv.RegisterRoutes(mux)

	llmCfg := config.LoadLLM()
	if llmCfg.IsEnabled() && fortnoxEnabled {
		mcpDeps, err := adapterfortnox.BuildMCPDeps(cfg, tokenStore, domain.TenantID("default"))
		if err != nil {
			log.Error("refusing to start", "err", err)
			os.Exit(1)
		}
		chatHandler := ui.NewChatHandler(mcpDeps, llmCfg, cfg.Mode, cfg.AllowsWrites, log)
		mux.HandleFunc("GET /chat", chatHandler.PageHandler)
		mux.HandleFunc("POST /chat", chatHandler.MessageHandler)
		log.Info("chat handler registered")
	} else {
		log.Info("chat handler disabled", "reason", "LLM_BASE_URL or DMABE_LLMAPI_KEY not set, or Fortnox not configured")
	}

	handler, err := secureHandler(mux, sessions, oidcCfg, oidcHandler, config.AllowUnauthenticated())
	if err != nil {
		log.Error("refusing to start", "err", err)
		os.Exit(1)
	}
	if oidcHandler == nil {
		log.Warn("SERVING WITHOUT AUTHENTICATION — every route is public",
			"opt_in", "COBALT_ALLOW_UNAUTHENTICATED")
	}

	if err := http.ListenAndServe(":"+port, handler); err != nil {
		log.Error("server failed", "err", err)
		os.Exit(1)
	}
}
