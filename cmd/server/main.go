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

	cfg, err := config.Load()
	if err != nil {
		log.Warn("Fortnox not configured — serving placeholder data", "err", err)
	}

	debtor := config.LoadDebtor()
	appCfg := config.LoadApp()

	log.Info("cobalt-dingo starting", "port", port, "fortnox_env", cfg.Env)

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

	if cfg.ClientID == "" {
		invoiceSource = fake.InvoiceSource{}
		enricher = fake.SupplierEnricher{}
	} else {
		var tokenStore domain.TokenStore
		if appCfg.DatabaseURL != "" {
			store, _ := postgres.NewStore(appCfg.DatabaseURL)
			tokenStore = postgres.NewTokenStore(store)
		} else {
			tokenStore = file.NewTokenStore()
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
	if claudeCfg.APIKey != "" {
		var tokenStore domain.TokenStore
		if appCfg.DatabaseURL != "" {
			store, _ := postgres.NewStore(appCfg.DatabaseURL)
			tokenStore = postgres.NewTokenStore(store)
		} else {
			tokenStore = file.NewTokenStore()
		}
		baseURL := cfg.BaseURL()
		gl := adapterfortnox.NewGeneralLedgerAdapter(baseURL, tokenStore)
		mcpDeps := mcpserver.Deps{
			TenantID:    defaultTenantID,
			SupplierLdg: adapterfortnox.NewSupplierLedgerAdapter(baseURL, tokenStore),
			CustomerLdg: adapterfortnox.NewCustomerLedgerAdapter(baseURL, tokenStore),
			GeneralLdg:  gl,
			ProjectLdg:  adapterfortnox.NewProjectLedgerAdapter(baseURL, tokenStore, gl),
			CostCtrLdg:  adapterfortnox.NewCostCenterLedgerAdapter(baseURL, tokenStore, gl),
			AssetReg:    adapterfortnox.NewAssetRegisterAdapter(baseURL, tokenStore),
			CompanyInf:  adapterfortnox.NewCompanyInfoAdapter(baseURL, tokenStore),
		}
		chatHandler := ui.NewChatHandler(mcpDeps, claudeCfg, log)
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
