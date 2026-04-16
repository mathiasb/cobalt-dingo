// Package main is the cobalt-dingo server entry point.
package main

import (
	"log/slog"
	"net/http"
	"os"

	"github.com/mathiasb/cobalt-dingo/internal/adapter/fake"
	"github.com/mathiasb/cobalt-dingo/internal/adapter/file"
	adapterfortnox "github.com/mathiasb/cobalt-dingo/internal/adapter/fortnox"
	"github.com/mathiasb/cobalt-dingo/internal/config"
	"github.com/mathiasb/cobalt-dingo/internal/domain"
	"github.com/mathiasb/cobalt-dingo/internal/ui"
)

const defaultTenantID = domain.TenantID("default")

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

	log.Info("cobalt-dingo starting", "port", port, "fortnox_env", cfg.Env)

	var (
		invoiceSource domain.InvoiceSource
		enricher      domain.SupplierEnricher
	)

	if cfg.ClientID == "" {
		invoiceSource = fake.InvoiceSource{}
		enricher = fake.SupplierEnricher{}
	} else {
		tokens := file.NewTokenStore()
		connector := adapterfortnox.NewConnector(cfg, tokens, log)
		invoiceSource = connector
		enricher = connector
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))

	srv := ui.NewServer(defaultTenantID, debtor, invoiceSource, enricher, log)
	srv.RegisterRoutes(mux)

	if err := http.ListenAndServe(":"+port, mux); err != nil {
		log.Error("server failed", "err", err)
		os.Exit(1)
	}
}
