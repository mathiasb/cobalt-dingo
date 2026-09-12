package auth

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mathiasb/cobalt-dingo/internal/config"
)

// discoveryServer serves the minimum OIDC discovery document go-oidc needs, so
// the real constructor can be exercised without an identity provider.
func discoveryServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                                srv.URL,
			"authorization_endpoint":                srv.URL + "/authorize",
			"token_endpoint":                        srv.URL + "/token",
			"jwks_uri":                              srv.URL + "/jwks",
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})
	return srv
}

// THE regression. NewOIDCHandler took a directory and never assigned it, so
// h.directory was nil in production while every test set the field directly on
// a hand-built struct — the constructor was never exercised at all.
//
// A login then completed state, nonce, token exchange and ID-token
// verification, and died at "could not establish who you are". Go permits an
// unused parameter, so nothing complained at compile time.
func TestNewOIDCHandler_wiresTheOwnerDirectory(t *testing.T) {
	srv := discoveryServer(t)
	dir := newFakeDirectory()

	h, err := NewOIDCHandler(
		context.Background(),
		config.OIDC{IssuerURL: srv.URL, ClientID: "cobalt-dingo", RedirectURL: srv.URL + "/auth/callback"},
		NewSessionManager("test-secret-that-is-long-enough"),
		config.ModeSandbox,
		dir,
		slog.New(slog.DiscardHandler),
	)

	require.NoError(t, err)
	require.NotNil(t, h.directory,
		"the constructor accepted a directory and dropped it — logins cannot resolve a credential owner")
	assert.Same(t, dir, h.directory, "and it must be the one passed in")
}

// The other constructor arguments deserve the same check, for the same reason:
// an accepted-and-dropped parameter is invisible to the compiler.
func TestNewOIDCHandler_wiresEveryDependency(t *testing.T) {
	srv := discoveryServer(t)
	sessions := NewSessionManager("test-secret-that-is-long-enough")

	h, err := NewOIDCHandler(
		context.Background(),
		config.OIDC{IssuerURL: srv.URL, ClientID: "cobalt-dingo", RedirectURL: srv.URL + "/auth/callback"},
		sessions,
		config.ModeProduction,
		newFakeDirectory(),
		slog.New(slog.DiscardHandler),
	)
	require.NoError(t, err)

	assert.Same(t, sessions, h.sessions, "sessions must be wired")
	assert.Equal(t, config.ModeProduction, h.defaultMode, "the default mode must be wired")
	assert.NotNil(t, h.verifier, "the verifier must be wired")
	assert.NotNil(t, h.log, "the logger must be wired")
	assert.Equal(t, "cobalt-dingo", h.oauth2cfg.ClientID, "the oauth config must be wired")
}
