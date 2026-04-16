// Package main is the cobalt-dingo server entry point.
package main

import (
	"log/slog"
	"net/http"
	"os"

	"github.com/mathiasb/cobalt-dingo/internal/config"
	"github.com/mathiasb/cobalt-dingo/internal/ui"
)

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

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))

	srv := ui.NewServer(cfg, debtor, log)
	srv.RegisterRoutes(mux)

	if err := http.ListenAndServe(":"+port, mux); err != nil {
		log.Error("server failed", "err", err)
		os.Exit(1)
	}
}
