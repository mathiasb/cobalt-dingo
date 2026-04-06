package main

import (
	"context"
	"crypto/rand"
	"flag"
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

// Fortnox scopes required by this application.
var fortnoxScopes = []string{
	"companyinformation",
	"bookkeeping",
	"invoice",
	"supplierinvoice",
	"print",
}

func main() {
	doAuth := flag.Bool("auth", false, "Run the OAuth2 authorization flow to obtain a Fortnox token")
	flag.Parse()

	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, nil)))

	if *doAuth {
		runAuth()
		return
	}

	runServer()
}

// runAuth performs the one-time OAuth2 Authorization Code Flow.
func runAuth() {
	slog.Info("starting OAuth2 authorization flow")

	cfg := auth.OAuthConfig{
		ClientID:     mustEnv("FORTNOX_CLIENT_ID"),
		ClientSecret: mustEnv("FORTNOX_CLIENT_SECRET"),
		RedirectURI:  mustEnv("FORTNOX_REDIRECT_URI"),
		Scopes:       fortnoxScopes,
	}

	authURL, state := auth.AuthorizationURL(cfg)

	fmt.Println()
	fmt.Println("Öppna följande URL i din webbläsare och logga in med ditt Fortnox-konto:")
	fmt.Println()
	fmt.Println(" ", authURL)
	fmt.Println()

	callbackAddr := extractCallbackAddr(cfg.RedirectURI)
	srv, err := auth.NewCallbackServer(callbackAddr)
	if err != nil {
		slog.Error("failed to start callback server", "err", err)
		os.Exit(1)
	}
	fmt.Printf("Väntar på svar från Fortnox (lyssnar på %s)...\n", callbackAddr)

	ctx, cancel := context.WithTimeout(context.Background(), 5*60*1e9) // 5 min
	defer cancel()

	result := srv.Wait(ctx)
	if result.Err != nil {
		slog.Error("authorization failed", "err", result.Err)
		os.Exit(1)
	}
	if err := auth.ValidateState(state, result.State); err != nil {
		slog.Error("state validation failed", "err", err)
		os.Exit(1)
	}

	token, err := auth.ExchangeCode(ctx, cfg, result.Code)
	if err != nil {
		slog.Error("token exchange failed", "err", err)
		os.Exit(1)
	}

	encKey, err := loadOrCreateKey()
	if err != nil {
		slog.Error("failed to load encryption key", "err", err)
		os.Exit(1)
	}
	store, err := auth.NewFileTokenStore(tokenPath(), encKey)
	if err != nil {
		slog.Error("failed to open token store", "err", err)
		os.Exit(1)
	}
	if err := store.Save(token); err != nil {
		slog.Error("failed to save token", "err", err)
		os.Exit(1)
	}

	fmt.Println()
	fmt.Println("✓ Token sparad. Du kan nu starta coo-agent utan -auth flaggan.")
}

// runServer starts the MCP server and scheduler.
func runServer() {
	slog.Info("starting coo-agent", "version", version)

	auditLog, err := audit.NewFileLogger(auditLogPath())
	if err != nil {
		slog.Error("failed to open audit log", "err", err)
		os.Exit(1)
	}
	defer auditLog.Close()

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

// --- helpers ---

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		slog.Error("required environment variable not set", "key", key)
		os.Exit(1)
	}
	return v
}

func auditLogPath() string {
	if p := os.Getenv("AUDIT_LOG_PATH"); p != '' {
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

// extractCallbackAddr extracts ":port" from a redirect URI like "http://localhost:8080/callback".
func extractCallbackAddr(redirectURI string) string {
	// Simple extraction: find the port from the URI.
	// Works for http://localhost:PORT/anything.
	for i := len("http://localhost"); i < len(redirectURI); i++ {
		if redirectURI[i] == ':' {
			end := i + 1
			for end < len(redirectURI) && redirectURI[end] >= '0' && redirectURI[end] <= '9' {
				end++
			}
			return ":" + redirectURI[i+1:end]
		}
	}
	return ":8080"
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
