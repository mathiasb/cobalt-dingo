// Package main is the HTTP server binary for coo-agent.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/joho/godotenv"
	"github.com/mathiasb/cobalt-dingo/internal/receipts/httpserver"
	"github.com/mathiasb/cobalt-dingo/internal/receipts/httpserver/ui/stub"
)

func main() {
	_ = godotenv.Load()

	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	addr := os.Getenv("LISTEN_ADDR")
	if addr == "" {
		addr = ":8080"
	}

	// The admin UI can refresh and revoke the Fortnox token, issue and revoke
	// API keys, and read the audit log. It does not start without credentials.
	auth, err := httpserver.BearerToken(os.Getenv("RECEIPTS_ADMIN_TOKEN"))
	if err != nil {
		slog.Error("refusing to start: RECEIPTS_ADMIN_TOKEN is missing or too short", "err", err)
		os.Exit(1)
	}

	// Stub backends — replaced by real PostgreSQL implementations in issue #2.
	srv, err := httpserver.New(httpserver.Config{
		Addr:   addr,
		Auth:   auth,
		Tokens: stub.ConnectedTokens(),
		Keys:   stub.SeedKeys(2),
		Audit:  stub.SeedAudit(20),
		Health: stub.AllHealthy(),
	})
	if err != nil {
		slog.Error("refusing to start", "err", err)
		os.Exit(1)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	slog.Info("starting coo-agent server (stub mode)")
	if err := srv.Start(ctx); err != nil {
		slog.Error("server error", "err", err)
		os.Exit(1)
	}
}
