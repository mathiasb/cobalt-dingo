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
	"github.com/mathiasb/cobalt-dingo/internal/domain"
	mcpserver "github.com/mathiasb/cobalt-dingo/internal/mcp"
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
	)

	// Wire postgres adapters when DATABASE_URL is set.
	var pgStore *postgres.Store
	var batchRepo domain.BatchRepository
	var tenantRepo domain.TenantRepository
	if appCfg.DatabaseURL != "" {
		var dbErr error
		pgStore, dbErr = postgres.NewStore(appCfg.DatabaseURL)
		if dbErr != nil {
			log.Error("postgres connect failed", "err", dbErr)
			os.Exit(1)
		}
		batchRepo = postgres.NewBatchRepo(pgStore)
		tenantRepo = postgres.NewTenantRepo(pgStore)
		log.Info("postgres connected")
	}

	pispSubmitter := pisp.NewStub(log)

	if !fortnoxEnabled {
		invoiceSource = fake.InvoiceSource{}
		enricher = fake.SupplierEnricher{}
	} else {
		var tokenStore domain.TokenStore
		if pgStore != nil {
			tokenStore = postgres.NewTokenStore(pgStore)
		} else {
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
		var err error
		oidcHandler, err = auth.NewOIDCHandler(context.Background(), oidcCfg, sessions, defaultMode, log)
		if err != nil {
			log.Error("OIDC setup failed — running without auth", "err", err)
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

	// Fortnox web-based OAuth connect (per user, per mode).
	// Loaded from all configured modes so users can connect sandbox + production.
	if pgStore != nil {
		tokenStore := postgres.NewTokenStore(pgStore)
		connector := ui.NewFortnoxConnector(config.LoadAllModes(), tokenStore, tenantRepo, log)
		connector.RegisterRoutes(mux)
		log.Info("fortnox connect routes registered")
	}

	srv := ui.NewServer(debtor, invoiceSource, enricher, batchSvc, sessions, log)
	srv.RegisterRoutes(mux)

	llmCfg := config.LoadLLM()
	if llmCfg.IsEnabled() && fortnoxEnabled {
		var tokenStore domain.TokenStore
		if pgStore != nil {
			tokenStore = postgres.NewTokenStore(pgStore)
		} else {
			tokenStore = file.NewTokenStore(cfg.Mode.TokenFile())
		}
		baseURL := cfg.BaseURL()
		readOnly := !cfg.AllowsWrites
		gl := adapterfortnox.NewGeneralLedgerAdapter(baseURL, tokenStore, readOnly)
		mcpDeps := mcpserver.Deps{
			TenantID:    domain.TenantID("default"),
			SupplierLdg: adapterfortnox.NewSupplierLedgerAdapter(baseURL, tokenStore, readOnly),
			CustomerLdg: adapterfortnox.NewCustomerLedgerAdapter(baseURL, tokenStore, readOnly),
			GeneralLdg:  gl,
			ProjectLdg:  adapterfortnox.NewProjectLedgerAdapter(baseURL, tokenStore, gl, readOnly),
			CostCtrLdg:  adapterfortnox.NewCostCenterLedgerAdapter(baseURL, tokenStore, gl, readOnly),
			AssetReg:    adapterfortnox.NewAssetRegisterAdapter(baseURL, tokenStore, readOnly),
			CompanyInf:  adapterfortnox.NewCompanyInfoAdapter(baseURL, tokenStore, readOnly),
		}
		chatHandler := ui.NewChatHandler(mcpDeps, llmCfg, cfg.Mode, cfg.AllowsWrites, log)
		mux.HandleFunc("GET /chat", chatHandler.PageHandler)
		mux.HandleFunc("POST /chat", chatHandler.MessageHandler)
		log.Info("chat handler registered")
	} else {
		log.Info("chat handler disabled", "reason", "LLM_BASE_URL or DMABE_LLMAPI_KEY not set, or Fortnox not configured")
	}

	// Wrap with auth middleware when OIDC is active.
	var handler http.Handler = mux
	if oidcHandler != nil {
		handler = sessions.AuthMiddleware(
			[]string{"/healthz", "/static/", "/auth/"},
			mux,
		)
	}

	if err := http.ListenAndServe(":"+port, handler); err != nil {
		log.Error("server failed", "err", err)
		os.Exit(1)
	}
}
