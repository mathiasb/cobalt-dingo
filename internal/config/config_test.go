package config

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMode_Predicates exercises the mode classification helpers that the
// Client write-gate and Taskfile depend on. If these drift, the safety
// guarantee drifts with them.
func TestMode_Predicates(t *testing.T) {
	tests := []struct {
		mode         Mode
		valid        bool
		sandbox      bool
		real         bool
		allowsWrites bool
		envPrefix    string
		tokenFile    string
	}{
		{ModeSandbox, true, true, false, true, "FORTNOX_SANDBOX_", ".fortnox-tokens-sandbox.json"},
		{ModeRealReadonly, true, false, true, false, "FORTNOX_REAL_RO_", ".fortnox-tokens-real-ro.json"},
		{Mode("bogus"), false, false, false, false, "", ""},
		{Mode(""), false, false, false, false, "", ""},
	}

	for _, tt := range tests {
		t.Run(string(tt.mode), func(t *testing.T) {
			assert.Equal(t, tt.valid, tt.mode.IsValid(), "IsValid")
			assert.Equal(t, tt.sandbox, tt.mode.IsSandbox(), "IsSandbox")
			assert.Equal(t, tt.real, tt.mode.IsReal(), "IsReal")
			assert.Equal(t, tt.allowsWrites, tt.mode.AllowsWrites(), "AllowsWrites")
			assert.Equal(t, tt.envPrefix, tt.mode.EnvPrefix(), "EnvPrefix")
			assert.Equal(t, tt.tokenFile, tt.mode.TokenFile(), "TokenFile")
		})
	}
}

// TestLoad_RequiresMode confirms that Load refuses to run when FORTNOX_MODE
// is unset. This is what prevents a missed env var from accidentally
// running a binary against the wrong company.
func TestLoad_RequiresMode(t *testing.T) {
	clearFortnoxEnv(t)

	_, err := Load()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "FORTNOX_MODE is not set")
}

// TestLoad_RejectsInvalidMode verifies the guard against typo'd modes.
func TestLoad_RejectsInvalidMode(t *testing.T) {
	clearFortnoxEnv(t)
	t.Setenv("FORTNOX_MODE", "production")

	_, err := Load()
	require.Error(t, err)
	assert.Contains(t, err.Error(), `"production" is not recognized`)
}

// TestLoad_RejectsMissingCredentials confirms each required field for the
// chosen mode is checked. A mode with partial credentials should never
// silently proceed — partial config is the kind of misconfiguration that
// could route a request to the wrong company.
func TestLoad_RejectsMissingCredentials(t *testing.T) {
	tests := []struct {
		name       string
		setup      func(t *testing.T)
		wantSubstr string
	}{
		{
			name: "sandbox missing CLIENT_ID",
			setup: func(t *testing.T) {
				clearFortnoxEnv(t)
				t.Setenv("FORTNOX_MODE", "sandbox")
				t.Setenv("FORTNOX_SANDBOX_CLIENT_SECRET", "s")
				t.Setenv("FORTNOX_SANDBOX_REDIRECT_URI", "http://localhost/cb")
			},
			wantSubstr: "FORTNOX_SANDBOX_CLIENT_ID is not set",
		},
		{
			name: "real_readonly missing CLIENT_SECRET",
			setup: func(t *testing.T) {
				clearFortnoxEnv(t)
				t.Setenv("FORTNOX_MODE", "real_readonly")
				t.Setenv("FORTNOX_REAL_RO_CLIENT_ID", "id")
				t.Setenv("FORTNOX_REAL_RO_REDIRECT_URI", "http://localhost/cb")
			},
			wantSubstr: "FORTNOX_REAL_RO_CLIENT_SECRET is not set",
		},
		{
			name: "sandbox missing REDIRECT_URI",
			setup: func(t *testing.T) {
				clearFortnoxEnv(t)
				t.Setenv("FORTNOX_MODE", "sandbox")
				t.Setenv("FORTNOX_SANDBOX_CLIENT_ID", "id")
				t.Setenv("FORTNOX_SANDBOX_CLIENT_SECRET", "s")
			},
			wantSubstr: "FORTNOX_SANDBOX_REDIRECT_URI is not set",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.setup(t)
			_, err := Load()
			require.Error(t, err)
			assert.True(t, strings.Contains(err.Error(), tt.wantSubstr),
				"error %q should contain %q", err.Error(), tt.wantSubstr)
		})
	}
}

// TestLoad_HappyPath_Sandbox confirms a fully-set sandbox config loads cleanly.
func TestLoad_HappyPath_Sandbox(t *testing.T) {
	clearFortnoxEnv(t)
	t.Setenv("FORTNOX_MODE", "sandbox")
	t.Setenv("FORTNOX_SANDBOX_CLIENT_ID", "sandbox-id")
	t.Setenv("FORTNOX_SANDBOX_CLIENT_SECRET", "sandbox-secret")
	t.Setenv("FORTNOX_SANDBOX_REDIRECT_URI", "http://localhost:8080/callback")
	t.Setenv("FORTNOX_SANDBOX_SCOPES", "supplier invoice")
	t.Setenv("FORTNOX_SANDBOX_INVOICE_INBOX", "inbox@example.com")

	cfg, err := Load()
	require.NoError(t, err)
	assert.Equal(t, ModeSandbox, cfg.Mode)
	assert.Equal(t, "sandbox-id", cfg.ClientID)
	assert.Equal(t, "sandbox-secret", cfg.ClientSecret)
	assert.True(t, cfg.IsSandbox())
	assert.True(t, cfg.Mode.AllowsWrites())
}

// TestLoad_HappyPath_RealReadonly confirms real_readonly loads from its
// own credential keys (no leakage from sandbox keys).
func TestLoad_HappyPath_RealReadonly(t *testing.T) {
	clearFortnoxEnv(t)
	t.Setenv("FORTNOX_MODE", "real_readonly")
	t.Setenv("FORTNOX_REAL_RO_CLIENT_ID", "real-id")
	t.Setenv("FORTNOX_REAL_RO_CLIENT_SECRET", "real-secret")
	t.Setenv("FORTNOX_REAL_RO_REDIRECT_URI", "http://localhost:8080/callback")

	// Sandbox creds present should be ignored — mode determines which set is read.
	t.Setenv("FORTNOX_SANDBOX_CLIENT_ID", "should-not-leak")

	cfg, err := Load()
	require.NoError(t, err)
	assert.Equal(t, ModeRealReadonly, cfg.Mode)
	assert.Equal(t, "real-id", cfg.ClientID, "must read from REAL_RO prefix, not SANDBOX")
	assert.False(t, cfg.IsSandbox())
	assert.False(t, cfg.Mode.AllowsWrites())
}

// clearFortnoxEnv unsets every Fortnox-related variable so the test starts
// from a known-empty state. t.Setenv restores the original values on
// cleanup, so this is safe.
func clearFortnoxEnv(t *testing.T) {
	t.Helper()
	keys := []string{
		"FORTNOX_MODE",
		"FORTNOX_SANDBOX_CLIENT_ID", "FORTNOX_SANDBOX_CLIENT_SECRET",
		"FORTNOX_SANDBOX_REDIRECT_URI", "FORTNOX_SANDBOX_SCOPES",
		"FORTNOX_SANDBOX_INVOICE_INBOX",
		"FORTNOX_REAL_RO_CLIENT_ID", "FORTNOX_REAL_RO_CLIENT_SECRET",
		"FORTNOX_REAL_RO_REDIRECT_URI", "FORTNOX_REAL_RO_SCOPES",
		"FORTNOX_REAL_RO_INVOICE_INBOX",
	}
	for _, k := range keys {
		t.Setenv(k, "")
	}
}
