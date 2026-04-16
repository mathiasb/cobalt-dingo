// Package config loads and validates typed application configuration from environment variables.
package config

import (
	"fmt"
	"os"
)

// Fortnox holds credentials and environment settings for the Fortnox API.
type Fortnox struct {
	ClientID     string
	ClientSecret string
	RedirectURI  string
	Scopes       string
	Env          string // "sandbox" | "production"
	InvoiceInbox string // Arkivplats email for incoming supplier invoices
}

// BaseURL returns the Fortnox REST API host (no path suffix).
// Both sandbox and production use the same host — sandbox is distinguished
// by the OAuth2 credentials used, not the URL.
func (f Fortnox) BaseURL() string {
	return "https://api.fortnox.se"
}

// IsSandbox reports whether this configuration targets the sandbox environment.
func (f Fortnox) IsSandbox() bool {
	return f.Env != "production"
}

// Load reads Fortnox configuration from environment variables.
// Returns an error if any required variable is missing.
func Load() (Fortnox, error) {
	cfg := Fortnox{
		ClientID:     os.Getenv("FORTNOX_CLIENT_ID"),
		ClientSecret: os.Getenv("FORTNOX_CLIENT_SECRET"),
		RedirectURI:  os.Getenv("FORTNOX_REDIRECT_URI"),
		Scopes:       os.Getenv("FORTNOX_SCOPES"),
		Env:          os.Getenv("FORTNOX_ENV"),
		InvoiceInbox: os.Getenv("FORTNOX_INVOICE_INBOX"),
	}
	if cfg.ClientID == "" {
		return Fortnox{}, fmt.Errorf("FORTNOX_CLIENT_ID is not set")
	}
	if cfg.ClientSecret == "" {
		return Fortnox{}, fmt.Errorf("FORTNOX_CLIENT_SECRET is not set")
	}
	if cfg.RedirectURI == "" {
		return Fortnox{}, fmt.Errorf("FORTNOX_REDIRECT_URI is not set")
	}
	if cfg.Env == "" {
		cfg.Env = "sandbox"
	}
	if cfg.Env != "sandbox" && cfg.Env != "production" {
		return Fortnox{}, fmt.Errorf("FORTNOX_ENV must be 'sandbox' or 'production', got %q", cfg.Env)
	}
	return cfg, nil
}
