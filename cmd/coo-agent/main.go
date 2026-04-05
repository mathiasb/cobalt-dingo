package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/mathiasb/coo-agent/internal/api"
	"github.com/mathiasb/coo-agent/internal/audit"
	"github.com/mathiasb/coo-agent/internal/auth"
	"github.com/mathiasb/coo-agent/internal/mcp"
	"github.com/mathiasb/coo-agent/internal/scheduler"
	"github.com/mathiasb/coo-agent/internal/validator"
)

var version = "dev"

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	slog.SetDefault(logger)

	slog.Info("starting coo-agent", "version", version)

	auditLog, err := audit.New(auditLogPath())
	if err != nil {
		slog.Error("failed to open audit log", "err", err)
		os.Exit(1)
	}
	defer auditLog.Close()

	tokenStore, err := auth.NewTokenStore(tokenPath())
	if err != nil {
		slog.Error("failed to open token store", "err", err)
		os.Exit(1)
	}

	fortnoxClient, err := api.New(api.Config{
		ClientID:     mustEnv("FORTNOX_CLIENT_ID"),
		ClientSecret: mustEnv("FORTNOX_CLIENT_SECRET"),
		RedirectURI:  mustEnv("FORTNOX_REDIRECT_URI"),
		Sandbox:      os.Getenv("FORTNOX_ENV") == "sandbox",
		TokenStore:   tokenStore,
		AuditLog:     auditLog,
	})
	if err != nil {
		slog.Error("failed to create Fortnox client", "err", err)
		os.Exit(1)
	}

	val := validator.New(fortnoxClient)

	sched := scheduler.New(fortnoxClient, auditLog)

	srv := mcp.NewServer(fortnoxClient, val, auditLog)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	go func() {
		if err := sched.Start(ctx); err != nil {
			slog.Error("scheduler error", "err", err)
		}
	}()

	slog.Info("MCP server listening on stdio")
	if err := srv.Start(ctx); err != nil {
		slog.Error("MCP server error", "err", err)
		os.Exit(1)
	}
}

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		slog.Error("required environment variable not set", "key", key)
		os.Exit(1)
	}
	return v
}

func auditLogPath() string {
	if p := os.Getenv("AUDIT_LOG_PATH"); p != "" {
		return p
	}
	home, _ := os.UserHomeDir()
	return home + "/.coo-agent/audit.log"
}

func tokenPath() string {
	if p := os.Getenv("TOKEN_PATH"); p != "" {
		return p
	}
	home, _ := os.UserHomeDir()
	return home + "/.coo-agent/tokens.enc"
}
