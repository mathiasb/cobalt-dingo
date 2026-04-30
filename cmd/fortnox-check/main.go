// Command fortnox-check verifies the Fortnox API connection and confirms
// the environment (sandbox vs production) by listing unpaid supplier invoices.
//
// Usage:
//
//	source .env && go run ./cmd/fortnox-check
package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"

	"github.com/mathiasb/cobalt-dingo/internal/config"
	"github.com/mathiasb/cobalt-dingo/internal/fortnox"
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))

	cfg, err := config.Load()
	if err != nil {
		log.Error("config", "err", err)
		os.Exit(1)
	}

	token, err := loadValidToken(cfg, log)
	if err != nil {
		log.Error("token", "err", err)
		os.Exit(1)
	}

	count, err := unpaidSupplierInvoiceCount(cfg.BaseURL(), token.AccessToken)
	if err != nil {
		log.Error("supplierinvoices", "err", err)
		os.Exit(1)
	}

	fmt.Println("─────────────────────────────────────")
	fmt.Printf("  Mode                 : %s\n", cfg.Mode.Label())
	fmt.Printf("  Token file           : %s\n", cfg.Mode.TokenFile())
	fmt.Printf("  Base URL             : %s\n", cfg.BaseURL())
	fmt.Printf("  Writes allowed       : %v\n", cfg.Mode.AllowsWrites())
	fmt.Printf("  Unpaid invoices      : %d\n", count)
	fmt.Println("─────────────────────────────────────")

	switch cfg.Mode {
	case config.ModeSandbox:
		fmt.Println("✓ Connected to SANDBOX — safe to write")
	case config.ModeRealReadonly:
		fmt.Println("⚠ Connected to LIVE Fortnox in READ-ONLY mode — no writes possible")
	}
}

func loadValidToken(cfg config.Fortnox, log *slog.Logger) (fortnox.Token, error) {
	tokenPath := cfg.Mode.TokenFile()
	t, err := fortnox.LoadToken(tokenPath)
	if err != nil {
		return fortnox.Token{}, fmt.Errorf("no saved token at %s — run fortnox-auth for mode %s: %w", tokenPath, cfg.Mode, err)
	}
	if t.Valid() {
		return t, nil
	}
	log.Info("access token expired, refreshing")
	t, err = fortnox.RefreshAccessToken(cfg.ClientID, cfg.ClientSecret, t.RefreshToken)
	if err != nil {
		return fortnox.Token{}, fmt.Errorf("refresh failed — re-run fortnox-auth for mode %s: %w", cfg.Mode, err)
	}
	if err := fortnox.SaveToken(tokenPath, t); err != nil {
		log.Warn("could not save refreshed token", "err", err)
	}
	return t, nil
}

func unpaidSupplierInvoiceCount(baseURL, token string) (int, error) {
	req, err := http.NewRequest(http.MethodGet, baseURL+"/3/supplierinvoices?filter=unpaid", nil)
	if err != nil {
		return 0, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("GET supplierinvoices: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}

	var envelope struct {
		MetaInformation struct {
			TotalResources int `json:"@TotalResources"`
		} `json:"MetaInformation"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return 0, fmt.Errorf("decode: %w", err)
	}
	return envelope.MetaInformation.TotalResources, nil
}
