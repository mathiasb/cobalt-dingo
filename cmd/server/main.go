// Package main is the cobalt-dingo server entry point.
package main

import (
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/mathiasb/cobalt-dingo/internal/adapter/fake"
	"github.com/mathiasb/cobalt-dingo/internal/adapter/file"
	adapterfortnox "github.com/mathiasb/cobalt-dingo/internal/adapter/fortnox"
	"github.com/mathiasb/cobalt-dingo/internal/adapter/pisp"
	"github.com/mathiasb/cobalt-dingo/internal/adapter/postgres"
	"github.com/mathiasb/cobalt-dingo/internal/config"
	"github.com/mathiasb/cobalt-dingo/internal/domain"
	mcpserver "github.com/mathiasb/cobalt-dingo/internal/mcp"
	"github.com/mathiasb/cobalt-dingo/internal/ui"
)

const defaultTenantID = domain.TenantID("default")

// supplierCacheTTL is how long IBAN/BIC lookups are cached per supplier.
const supplierCacheTTL = 5 * time.Minute

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	// Fortnox is optional for the server: when FORTNOX_MODE is unset we run
	// in dev mode with fake adapters. When set, the mode is strict — config
	// loading rejects missing credentials so we fail fast on misconfiguration.
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
	var batchRepo domain.BatchRepository
	var tenantRepo domain.TenantRepository
	if appCfg.DatabaseURL != "" {
		store, dbErr := postgres.NewStore(appCfg.DatabaseURL)
		if dbErr != nil {
			log.Error("postgres connect failed", "err", dbErr)
			os.Exit(1)
		}
		batchRepo = postgres.NewBatchRepo(store)
		tenantRepo = postgres.NewTenantRepo(store)
		log.Info("postgres connected")
	}

	pispSubmitter := pisp.NewStub(log)

	if !fortnoxEnabled {
		invoiceSource = fake.InvoiceSource{}
		enricher = fake.SupplierEnricher{}
	} else {
		var tokenStore domain.TokenStore
		if appCfg.DatabaseURL != "" {
			store, _ := postgres.NewStore(appCfg.DatabaseURL)
			tokenStore = postgres.NewTokenStore(store)
		} else {
			tokenStore = file.NewTokenStore(cfg.Mode.TokenFile())
		}
		connector := adapterfortnox.NewConnector(cfg, tokenStore, log)
		invoiceSource = connector
		// Wrap with cross-request cache; per-request dedup still happens in handler.
		enricher = adapterfortnox.NewCachingEnricher(connector, supplierCacheTTL)
		erpWriter = adapterfortnox.NewERPWriter(connector)
	}

	if batchRepo != nil && tenantRepo != nil {
		batchSvc = domain.NewBatchService(batchRepo, tenantRepo, pispSubmitter, erpWriter)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))

	srv := ui.NewServer(defaultTenantID, debtor, invoiceSource, enricher, batchSvc, log)
	srv.RegisterRoutes(mux)

	claudeCfg := config.LoadClaude()
	if claudeCfg.APIKey != "" && fortnoxEnabled {
		var tokenStore domain.TokenStore
		if appCfg.DatabaseURL != "" {
			store, _ := postgres.NewStore(appCfg.DatabaseURL)
			tokenStore = postgres.NewTokenStore(store)
		} else {
			tokenStore = file.NewTokenStore(cfg.Mode.TokenFile())
		}
		baseURL := cfg.BaseURL()
		readOnly := !cfg.Mode.AllowsWrites()
		gl := adapterfortnox.NewGeneralLedgerAdapter(baseURL, tokenStore, readOnly)
		mcpDeps := mcpserver.Deps{
			TenantID:    defaultTenantID,
			SupplierLdg: adapterfortnox.NewSupplierLedgerAdapter(baseURL, tokenStore, readOnly),
			CustomerLdg: adapterfortnox.NewCustomerLedgerAdapter(baseURL, tokenStore, readOnly),
			GeneralLdg:  gl,
			ProjectLdg:  adapterfortnox.NewProjectLedgerAdapter(baseURL, tokenStore, gl, readOnly),
			CostCtrLdg:  adapterfortnox.NewCostCenterLedgerAdapter(baseURL, tokenStore, gl, readOnly),
			AssetReg:    adapterfortnox.NewAssetRegisterAdapter(baseURL, tokenStore, readOnly),
			CompanyInf:  adapterfortnox.NewCompanyInfoAdapter(baseURL, tokenStore, readOnly),
		}
		chatHandler := ui.NewChatHandler(mcpDeps, claudeCfg, cfg.Mode, log)
		mux.HandleFunc("GET /chat", chatHandler.PageHandler)
		mux.HandleFunc("POST /chat", chatHandler.MessageHandler)
		log.Info("chat handler registered")
	} else {
		log.Info("chat handler disabled — set ANTHROPIC_API_KEY to enable")
	}

	if err := http.ListenAndServe(":"+port, mux); err != nil {
		log.Error("server failed", "err", err)
		os.Exit(1)
	}
}
