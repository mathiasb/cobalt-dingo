// Package main is the MCP server binary for coo-agent.
package main

import (
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/mathiasb/coo-agent/internal/api"
	"github.com/mathiasb/coo-agent/internal/audit"
	"github.com/mathiasb/coo-agent/internal/auth"
	internalmcp "github.com/mathiasb/coo-agent/internal/mcp"
	"github.com/mathiasb/coo-agent/internal/validator"
)

var version = "dev"

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, nil)))
	slog.Info("starting coo-agent", "version", version)

	auditLog, err := audit.NewFileLogger(auditLogPath())
	if err != nil {
		slog.Error("failed to open audit log", "err", err)
		os.Exit(1)
	}
	defer func() { _ = auditLog.Close() }()

	encKey, err := loadOrCreateKey()
	if err != nil {
		slog.Error("failed to load encryption key", "err", err)
		os.Exit(1)
	}
	tokenStore, err := auth.NewFileTokenStore(tokenPath(), encKey)
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

	val, err := validator.New(fortnoxClient)
	if err != nil {
		slog.Error("failed to create validator", "err", err)
		os.Exit(1)
	}
	_ = val // used via fortnoxClient in MCP tools

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	mcpServer := internalmcp.New(fortnoxClient)
	slog.Info("starting MCP server over stdio")
	if err := mcpServer.ServeStdio(ctx); err != nil {
		slog.Error("MCP server error", "err", err)
		os.Exit(1)
	}
	slog.Info("shutting down")
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

// loadOrCreateKey reads the AES-256 key from disk, generating a new one if absent.
// TODO: replace with zalando/go-keyring for system keyring integration.
func loadOrCreateKey() ([]byte, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	keyPath := filepath.Join(home, ".coo-agent", "master.key")
	if err := os.MkdirAll(filepath.Dir(keyPath), 0o700); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(keyPath)
	if err == nil && len(data) == 32 {
		return data, nil
	}
	key := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, fmt.Errorf("generate key: %w", err)
	}
	if err := os.WriteFile(keyPath, key, 0o600); err != nil {
		return nil, fmt.Errorf("write key: %w", err)
	}
	return key, nil
}
