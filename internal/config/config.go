// Package config loads and validates typed application configuration from environment variables.
package config

import (
	"fmt"
	"os"
)

// Mode determines which Fortnox environment + capability this process targets.
// It controls credential lookup keys, token file paths, and whether write
// methods on fortnox.Client are permitted at runtime.
//
// real_readwrite is reserved for a future milestone — when added, it will
// share the EnvPrefix/TokenFile shape so write capability against live data
// is opt-in via mode change rather than implicit configuration.
type Mode string

const (
	// ModeSandbox targets the Fortnox sandbox via the SANDBOX-prefixed
	// connected app. Writes are permitted; the sandbox is the only place
	// e2e-seed and e2e-teardown will run.
	ModeSandbox Mode = "sandbox"

	// ModeRealReadonly targets the live Fortnox via a connected app
	// configured with read-only OAuth scopes. Client-side enforcement
	// (Client.do refusing non-GET) provides defense in depth on top of
	// the OAuth gate.
	ModeRealReadonly Mode = "real_readonly"
)

// IsValid reports whether m is a recognized mode.
func (m Mode) IsValid() bool {
	switch m {
	case ModeSandbox, ModeRealReadonly:
		return true
	}
	return false
}

// IsSandbox reports whether m targets the Fortnox sandbox environment.
func (m Mode) IsSandbox() bool { return m == ModeSandbox }

// IsReal reports whether m targets the live Fortnox environment.
func (m Mode) IsReal() bool { return m == ModeRealReadonly }

// AllowsWrites reports whether write requests (POST/PUT/DELETE/PATCH) are
// permitted under this mode.
func (m Mode) AllowsWrites() bool { return m == ModeSandbox }

// EnvPrefix returns the env-var prefix used to look up Fortnox credentials
// for this mode (e.g. "FORTNOX_SANDBOX_").
func (m Mode) EnvPrefix() string {
	switch m {
	case ModeSandbox:
		return "FORTNOX_SANDBOX_"
	case ModeRealReadonly:
		return "FORTNOX_REAL_RO_"
	}
	return ""
}

// TokenFile returns the path to the OAuth token cache for this mode.
func (m Mode) TokenFile() string {
	switch m {
	case ModeSandbox:
		return ".fortnox-tokens-sandbox.json"
	case ModeRealReadonly:
		return ".fortnox-tokens-real-ro.json"
	}
	return ""
}

// Label returns a human-readable name for banners and logs.
func (m Mode) Label() string {
	switch m {
	case ModeSandbox:
		return "SANDBOX"
	case ModeRealReadonly:
		return "REAL (read-only)"
	}
	return string(m)
}

// Fortnox holds credentials and the active mode for the Fortnox API.
type Fortnox struct {
	Mode         Mode
	ClientID     string
	ClientSecret string
	RedirectURI  string
	Scopes       string
	InvoiceInbox string // Arkivplats email for incoming supplier invoices
}

// BaseURL returns the Fortnox REST API host (no path suffix). Sandbox and
// live use the same host — the environment is distinguished by the OAuth
// credentials, not the URL.
func (f Fortnox) BaseURL() string {
	return "https://api.fortnox.se"
}

// IsSandbox reports whether this configuration targets the sandbox.
// Convenience shortcut around f.Mode.IsSandbox().
func (f Fortnox) IsSandbox() bool { return f.Mode.IsSandbox() }

// App holds general application configuration.
type App struct {
	DatabaseURL string // PostgreSQL connection string; empty means no DB configured
	Port        string
}

// LoadApp reads general application configuration from environment variables.
func LoadApp() App {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	return App{
		DatabaseURL: os.Getenv("DATABASE_URL"),
		Port:        port,
	}
}

// Debtor holds the paying entity's bank account details used in PAIN.001 batches.
type Debtor struct {
	Name string
	IBAN string
	BIC  string
}

// LoadDebtor reads debtor config from COBALT_DEBTOR_NAME / _IBAN / _BIC.
// Returns a placeholder when any value is absent (development mode).
func LoadDebtor() Debtor {
	name := os.Getenv("COBALT_DEBTOR_NAME")
	iban := os.Getenv("COBALT_DEBTOR_IBAN")
	bic := os.Getenv("COBALT_DEBTOR_BIC")
	if name == "" || iban == "" || bic == "" {
		return Debtor{Name: "Cobalt Dingo AB", IBAN: "SE4550000000058398257466", BIC: "ESSESESS"}
	}
	return Debtor{Name: name, IBAN: iban, BIC: bic}
}

// Claude holds Claude API configuration for the chat interface.
type Claude struct {
	APIKey string
	Model  string
}

// LoadClaude reads Claude config from environment variables.
func LoadClaude() Claude {
	model := os.Getenv("CLAUDE_MODEL")
	if model == "" {
		model = "claude-sonnet-4-6"
	}
	return Claude{
		APIKey: os.Getenv("ANTHROPIC_API_KEY"),
		Model:  model,
	}
}

// Load reads Fortnox configuration based on FORTNOX_MODE. It returns an
// error if FORTNOX_MODE is unset, invalid, or any required credential for
// the chosen mode is missing.
//
// Binaries that need a dev-mode fallback (cmd/server) should check
// os.Getenv("FORTNOX_MODE") themselves before calling Load.
func Load() (Fortnox, error) {
	raw := os.Getenv("FORTNOX_MODE")
	if raw == "" {
		return Fortnox{}, fmt.Errorf("FORTNOX_MODE is not set — must be %q or %q", ModeSandbox, ModeRealReadonly)
	}
	mode := Mode(raw)
	if !mode.IsValid() {
		return Fortnox{}, fmt.Errorf("FORTNOX_MODE %q is not recognized — must be %q or %q", raw, ModeSandbox, ModeRealReadonly)
	}
	p := mode.EnvPrefix()
	cfg := Fortnox{
		Mode:         mode,
		ClientID:     os.Getenv(p + "CLIENT_ID"),
		ClientSecret: os.Getenv(p + "CLIENT_SECRET"),
		RedirectURI:  os.Getenv(p + "REDIRECT_URI"),
		Scopes:       os.Getenv(p + "SCOPES"),
		InvoiceInbox: os.Getenv(p + "INVOICE_INBOX"),
	}
	if cfg.ClientID == "" {
		return Fortnox{}, fmt.Errorf("%sCLIENT_ID is not set (required for FORTNOX_MODE=%s)", p, mode)
	}
	if cfg.ClientSecret == "" {
		return Fortnox{}, fmt.Errorf("%sCLIENT_SECRET is not set (required for FORTNOX_MODE=%s)", p, mode)
	}
	if cfg.RedirectURI == "" {
		return Fortnox{}, fmt.Errorf("%sREDIRECT_URI is not set (required for FORTNOX_MODE=%s)", p, mode)
	}
	return cfg, nil
}
