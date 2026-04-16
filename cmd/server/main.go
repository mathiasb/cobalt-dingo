// Package main is the cobalt-dingo server entry point.
package main

import (
	"log/slog"
	"net/http"
	"os"

	"github.com/mathiasb/cobalt-dingo/internal/ui"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Info("cobalt-dingo starting", "port", port)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	ui.RegisterRoutes(mux)

	if err := http.ListenAndServe(":"+port, mux); err != nil {
		log.Error("server failed", "err", err)
		os.Exit(1)
	}
}
