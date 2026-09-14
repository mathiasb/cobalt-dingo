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
		mode       Mode
		valid      bool
		sandbox    bool
		production bool
		envPrefix  string
		tokenFile  string
	}{
		{ModeSandbox, true, true, false, "FORTNOX_SANDBOX_", ".fortnox-tokens-sandbox.json"},
		{ModeProduction, true, false, true, "FORTNOX_PRODUCTION_", ".fortnox-tokens-production.json"},
		{Mode("bogus"), false, false, false, "", ""},
		{Mode(""), false, false, false, "", ""},
	}

	for _, tt := range tests {
		t.Run(string(tt.mode), func(t *testing.T) {
			assert.Equal(t, tt.valid, tt.mode.IsValid(), "IsValid")
			assert.Equal(t, tt.sandbox, tt.mode.IsSandbox(), "IsSandbox")
			assert.Equal(t, tt.production, tt.mode.IsProduction(), "IsProduction")
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
	t.Setenv("FORTNOX_MODE", "real_readonly")

	_, err := Load()
	require.Error(t, err)
	assert.Contains(t, err.Error(), `"real_readonly" is not recognized`)
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
			wantSubstr: "neither FORTNOX_SANDBOX_CLIENT_ID nor FORTNOX_CLIENT_ID is set",
		},
		{
			name: "production missing CLIENT_SECRET",
			setup: func(t *testing.T) {
				clearFortnoxEnv(t)
				t.Setenv("FORTNOX_MODE", "production")
				t.Setenv("FORTNOX_PRODUCTION_CLIENT_ID", "id")
				t.Setenv("FORTNOX_PRODUCTION_REDIRECT_URI", "http://localhost/cb")
			},
			wantSubstr: "neither FORTNOX_PRODUCTION_CLIENT_SECRET nor FORTNOX_CLIENT_SECRET is set",
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
	assert.False(t, cfg.AllowsWrites,
		"sandbox no longer implies writes: the mode picks credentials, not an environment")
}

// TestLoad_HappyPath_Production confirms production loads from its
// own credential keys (no leakage from sandbox keys).
func TestLoad_HappyPath_Production(t *testing.T) {
	clearFortnoxEnv(t)
	t.Setenv("FORTNOX_MODE", "production")
	t.Setenv("FORTNOX_PRODUCTION_CLIENT_ID", "prod-id")
	t.Setenv("FORTNOX_PRODUCTION_CLIENT_SECRET", "prod-secret")
	t.Setenv("FORTNOX_PRODUCTION_REDIRECT_URI", "http://localhost:8080/callback")

	t.Setenv("FORTNOX_SANDBOX_CLIENT_ID", "should-not-leak")

	cfg, err := Load()
	require.NoError(t, err)
	assert.Equal(t, ModeProduction, cfg.Mode)
	assert.Equal(t, "prod-id", cfg.ClientID, "must read from PRODUCTION prefix, not SANDBOX")
	assert.False(t, cfg.IsSandbox())
	assert.False(t, cfg.AllowsWrites)
}

// clearFortnoxEnv unsets every Fortnox-related variable so the test starts
// from a known-empty state. t.Setenv restores the original values on
// cleanup, so this is safe.
func clearFortnoxEnv(t *testing.T) {
	t.Helper()
	keys := []string{
		"FORTNOX_MODE",
		"FORTNOX_CLIENT_ID", "FORTNOX_CLIENT_SECRET",
		"FORTNOX_SANDBOX_CLIENT_ID", "FORTNOX_SANDBOX_CLIENT_SECRET",
		"FORTNOX_SANDBOX_REDIRECT_URI", "FORTNOX_SANDBOX_SCOPES",
		"FORTNOX_SANDBOX_INVOICE_INBOX",
		"FORTNOX_PRODUCTION_CLIENT_ID", "FORTNOX_PRODUCTION_CLIENT_SECRET",
		"FORTNOX_PRODUCTION_REDIRECT_URI", "FORTNOX_PRODUCTION_SCOPES",
		"FORTNOX_PRODUCTION_INVOICE_INBOX",
		"FORTNOX_PRODUCTION_ALLOW_WRITES",
		"FORTNOX_SANDBOX_ALLOW_WRITES",
	}
	for _, k := range keys {
		t.Setenv(k, "")
	}
}

func TestLoadLLM_Defaults(t *testing.T) {
	t.Setenv("LLM_BASE_URL", "https://llm-api.example.com")
	t.Setenv("DMABE_LLMAPI_KEY", "test-key")
	// LLM_DEFAULT_MODEL unset → should default to "iguana/gemma4-31b"
	// LLM_ESCALATION_MODEL unset → should be ""

	cfg := LoadLLM()

	assert.Equal(t, "https://llm-api.example.com", cfg.BaseURL)
	assert.Equal(t, "test-key", cfg.APIKey)
	assert.Equal(t, "iguana/gemma4-31b", cfg.DefaultModel)
	assert.Equal(t, "", cfg.EscalationModel)
}

func TestLoadLLM_Explicit(t *testing.T) {
	t.Setenv("LLM_BASE_URL", "https://llm-api.example.com")
	t.Setenv("DMABE_LLMAPI_KEY", "sk-abc")
	t.Setenv("LLM_DEFAULT_MODEL", "koala/phi4-14b")
	t.Setenv("LLM_ESCALATION_MODEL", "berget/llama-3.3-70b")

	cfg := LoadLLM()

	assert.Equal(t, "koala/phi4-14b", cfg.DefaultModel)
	assert.Equal(t, "berget/llama-3.3-70b", cfg.EscalationModel)
}

func TestLoadLLM_Empty(t *testing.T) {
	t.Setenv("LLM_BASE_URL", "")
	t.Setenv("DMABE_LLMAPI_KEY", "")
	t.Setenv("LLM_DEFAULT_MODEL", "")
	t.Setenv("LLM_ESCALATION_MODEL", "")

	cfg := LoadLLM()

	assert.Empty(t, cfg.BaseURL)
	assert.Empty(t, cfg.APIKey)
	assert.False(t, cfg.IsEnabled())
}

func TestLLM_IsEnabled(t *testing.T) {
	tests := []struct {
		name    string
		cfg     LLM
		enabled bool
	}{
		{"all set", LLM{BaseURL: "http://x", APIKey: "k", DefaultModel: "m"}, true},
		{"missing BaseURL", LLM{APIKey: "k", DefaultModel: "m"}, false},
		{"missing APIKey", LLM{BaseURL: "http://x", DefaultModel: "m"}, false},
		{"missing DefaultModel", LLM{BaseURL: "http://x", APIKey: "k"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.enabled, tt.cfg.IsEnabled())
		})
	}
}

// TestFortnox_BaseURL covers the injectable-host seam added for cobalt-dingo#30.
// Production must keep resolving to the live Fortnox host with no configuration
// present; an explicit override exists so integration tests can point the real
// adapter.Connector at an httptest server.
func TestFortnox_BaseURL(t *testing.T) {
	tests := []struct {
		name string
		cfg  Fortnox
		want string
	}{
		{"defaults to the live host", Fortnox{}, "https://api.fortnox.se"},
		{"production mode is unchanged", Fortnox{Mode: ModeProduction}, "https://api.fortnox.se"},
		{"sandbox shares the live host", Fortnox{Mode: ModeSandbox}, "https://api.fortnox.se"},
		{"override wins when set", Fortnox{BaseURLOverride: "http://127.0.0.1:1234"}, "http://127.0.0.1:1234"},
		{"empty override is ignored", Fortnox{BaseURLOverride: ""}, "https://api.fortnox.se"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.cfg.BaseURL())
		})
	}
}

// TestLoad_DoesNotPopulateBaseURLOverride is the guard that keeps the test seam
// from becoming a production redirect vector: no environment variable may ever
// set BaseURLOverride, so a live process cannot be pointed away from Fortnox.
func TestLoad_DoesNotPopulateBaseURLOverride(t *testing.T) {
	t.Setenv("FORTNOX_MODE", "production")
	t.Setenv("FORTNOX_PRODUCTION_CLIENT_ID", "id")
	t.Setenv("FORTNOX_PRODUCTION_CLIENT_SECRET", "secret")
	t.Setenv("FORTNOX_PRODUCTION_REDIRECT_URI", "https://example.com/callback")
	t.Setenv("FORTNOX_BASE_URL", "http://evil.example.com")
	t.Setenv("FORTNOX_BASE_URL_OVERRIDE", "http://evil.example.com")

	cfg, err := Load()

	require.NoError(t, err)
	assert.Empty(t, cfg.BaseURLOverride)
	assert.Equal(t, "https://api.fortnox.se", cfg.BaseURL())
}

// "Sandbox" names a set of credentials, not a safe place. Both modes call
// api.fortnox.se, and a Fortnox Developer licence puts test companies under the
// licence holder's own organisation number — so FORTNOX_MODE=sandbox says
// nothing about whose books a token opens. It must therefore not be the thing
// that grants write access.
func TestLoad_sandboxIsReadOnlyUnlessWritesAreExplicitlyEnabled(t *testing.T) {
	clearFortnoxEnv(t)
	t.Setenv("FORTNOX_MODE", "sandbox")
	t.Setenv("FORTNOX_SANDBOX_CLIENT_ID", "sandbox-id")
	t.Setenv("FORTNOX_SANDBOX_CLIENT_SECRET", "sandbox-secret")
	t.Setenv("FORTNOX_SANDBOX_REDIRECT_URI", "http://localhost:8080/callback")

	cfg, err := Load()
	require.NoError(t, err)
	assert.False(t, cfg.AllowsWrites, "an unset FORTNOX_SANDBOX_ALLOW_WRITES must mean read-only")
}

func TestLoad_sandboxAllowsWritesOnlyWhenAskedFor(t *testing.T) {
	clearFortnoxEnv(t)
	t.Setenv("FORTNOX_MODE", "sandbox")
	t.Setenv("FORTNOX_SANDBOX_CLIENT_ID", "sandbox-id")
	t.Setenv("FORTNOX_SANDBOX_CLIENT_SECRET", "sandbox-secret")
	t.Setenv("FORTNOX_SANDBOX_REDIRECT_URI", "http://localhost:8080/callback")

	for _, tc := range []struct {
		value string
		want  bool
	}{
		{"true", true},
		{"", false},
		{"yes", false},
		{"TRUE", false},
		{"1", false},
	} {
		t.Setenv("FORTNOX_SANDBOX_ALLOW_WRITES", tc.value)
		cfg, err := Load()
		require.NoError(t, err)
		assert.Equalf(t, tc.want, cfg.AllowsWrites, "FORTNOX_SANDBOX_ALLOW_WRITES=%q", tc.value)
	}
}

// LoadAllModes feeds the running server, which is the deployment that faces
// the internet and holds several tenants. It must default the same way.
func TestLoadAllModes_sandboxIsReadOnlyByDefault(t *testing.T) {
	clearFortnoxEnv(t)
	t.Setenv("FORTNOX_SANDBOX_CLIENT_ID", "sandbox-id")
	t.Setenv("FORTNOX_SANDBOX_CLIENT_SECRET", "sandbox-secret")
	t.Setenv("FORTNOX_SANDBOX_REDIRECT_URI", "http://localhost:8080/callback")

	modes, _ := LoadAllModes()
	sandbox, ok := modes[ModeSandbox]
	require.True(t, ok)
	assert.False(t, sandbox.AllowsWrites,
		"the deployed server must not be writable merely because its mode is called sandbox")
}

// The Fortnox Developer Portal has ONE integration per app — the same client id
// and secret authorize against a test company and a real one, and which company
// was picked on the consent screen is the only difference. Mathias, 2026-09-14:
// "its THE SAME client id and secret for the app, irrespective of if I choose to
// use it with a test/sandbox company or my prod company."
//
// Requiring a per-mode pair meant two stored copies of one secret, which is a
// rotation hazard: rotate one, and the other mode silently keeps the old value
// until it fails at token exchange.
func TestLoadAllModes_sharedCredentialConfiguresBothModes(t *testing.T) {
	clearFortnoxEnv(t)
	t.Setenv("FORTNOX_CLIENT_ID", "shared-id")
	t.Setenv("FORTNOX_CLIENT_SECRET", "shared-secret")
	t.Setenv("FORTNOX_SANDBOX_REDIRECT_URI", "https://books.example/callback")
	t.Setenv("FORTNOX_PRODUCTION_REDIRECT_URI", "https://books.example/callback")

	modes, incomplete := LoadAllModes()
	assert.Empty(t, incomplete)

	for _, m := range []Mode{ModeSandbox, ModeProduction} {
		cfg, ok := modes[m]
		require.Truef(t, ok, "%s must be configured from the shared credential", m)
		assert.Equal(t, "shared-id", cfg.ClientID)
		assert.Equal(t, "shared-secret", cfg.ClientSecret)
	}
}

// A per-mode value still wins, for the case where someone genuinely registers
// two integrations. The shared credential is the default, not the only option.
func TestLoadAllModes_perModeCredentialOverridesTheSharedOne(t *testing.T) {
	clearFortnoxEnv(t)
	t.Setenv("FORTNOX_CLIENT_ID", "shared-id")
	t.Setenv("FORTNOX_CLIENT_SECRET", "shared-secret")
	t.Setenv("FORTNOX_SANDBOX_REDIRECT_URI", "https://books.example/callback")
	t.Setenv("FORTNOX_PRODUCTION_REDIRECT_URI", "https://books.example/callback")
	t.Setenv("FORTNOX_PRODUCTION_CLIENT_ID", "own-prod-id")
	t.Setenv("FORTNOX_PRODUCTION_CLIENT_SECRET", "own-prod-secret")

	modes, _ := LoadAllModes()
	assert.Equal(t, "shared-id", modes[ModeSandbox].ClientID)
	assert.Equal(t, "own-prod-id", modes[ModeProduction].ClientID,
		"a mode with its own integration must not be overridden by the shared one")
	assert.Equal(t, "own-prod-secret", modes[ModeProduction].ClientSecret)
}

// Load() feeds the CLI tools, which must resolve the same way as the server.
func TestLoad_usesTheSharedCredentialWhenNoPerModeOneIsSet(t *testing.T) {
	clearFortnoxEnv(t)
	t.Setenv("FORTNOX_MODE", "production")
	t.Setenv("FORTNOX_CLIENT_ID", "shared-id")
	t.Setenv("FORTNOX_CLIENT_SECRET", "shared-secret")
	t.Setenv("FORTNOX_PRODUCTION_REDIRECT_URI", "https://books.example/callback")

	cfg, err := Load()
	require.NoError(t, err)
	assert.Equal(t, "shared-id", cfg.ClientID)
	assert.Equal(t, "shared-secret", cfg.ClientSecret)
}

// Half a shared credential is still half a credential. The existing rule — a
// mode is not offered unless it has id, secret and redirect — must not be
// weakened by the fallback.
func TestLoadAllModes_sharedIdWithoutSharedSecretIsIncompleteNotOffered(t *testing.T) {
	clearFortnoxEnv(t)
	t.Setenv("FORTNOX_CLIENT_ID", "shared-id")
	t.Setenv("FORTNOX_SANDBOX_REDIRECT_URI", "https://books.example/callback")

	modes, incomplete := LoadAllModes()
	_, ok := modes[ModeSandbox]
	assert.False(t, ok, "a mode missing the secret must not be offered")
	assert.NotEmpty(t, incomplete, "and it must say why")
}
