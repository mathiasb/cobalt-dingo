package httpserver_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mathiasb/cobalt-dingo/internal/receipts/httpserver"
	"github.com/mathiasb/cobalt-dingo/internal/receipts/httpserver/ui/stub"
)

func backends() httpserver.Config {
	return httpserver.Config{
		Addr:   ":0",
		Tokens: stub.ConnectedTokens(),
		Keys:   stub.SeedKeys(2),
		Audit:  stub.SeedAudit(20),
		Health: stub.AllHealthy(),
	}
}

const testToken = "a-token-long-enough-to-be-plausible"

// The admin UI mounts token refresh/revoke, API-key create/revoke and the audit
// log. Constructing it without an authenticator must fail, not produce a server
// that someone can Start.
//
// Deliberately a construction error rather than a config default: an
// unauthenticated server starts perfectly well and answers every request with a
// 200, so nothing downstream reports the difference. A default that can be
// turned off is eventually turned off by someone who does not know what it
// protects.
func TestNew_RefusesWithoutAnAuthenticator(t *testing.T) {
	srv, err := httpserver.New(backends())

	require.ErrorIs(t, err, httpserver.ErrNoAuthenticator)
	assert.Nil(t, srv, "no server may be returned that could be Started by accident")
}

func TestNew_AcceptsAnAuthenticator(t *testing.T) {
	cfg := backends()
	auth, err := httpserver.BearerToken(testToken)
	require.NoError(t, err)
	cfg.Auth = auth

	srv, err := httpserver.New(cfg)
	require.NoError(t, err)
	assert.NotNil(t, srv)
}

// An empty or trivially short token is the same hole wearing a config value.
func TestBearerToken_RejectsWeakTokens(t *testing.T) {
	for _, tok := range []string{"", "short", "0123456789012345678901"} {
		t.Run(tok, func(t *testing.T) {
			a, err := httpserver.BearerToken(tok)
			require.Error(t, err, "a token this weak must not be accepted as an authenticator")
			assert.Nil(t, a)
		})
	}
}

func serve(t *testing.T, req *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	cfg := backends()
	auth, err := httpserver.BearerToken(testToken)
	require.NoError(t, err)
	cfg.Auth = auth

	srv, err := httpserver.New(cfg)
	require.NoError(t, err)

	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	return w
}

// Every admin route must be behind the authenticator. Table-driven because the
// failure mode being guarded is a route added later that misses the middleware.
func TestAdminRoutesRequireCredentials(t *testing.T) {
	routes := []struct{ method, path string }{
		{http.MethodGet, "/ui/"},
		{http.MethodGet, "/ui/auth"},
		{http.MethodPost, "/ui/token/refresh"},
		{http.MethodDelete, "/ui/token"},
		{http.MethodGet, "/ui/keys"},
		{http.MethodGet, "/ui/keys/new"},
		{http.MethodPost, "/ui/keys"},
		{http.MethodDelete, "/ui/keys/abc"},
		{http.MethodGet, "/ui/audit"},
		{http.MethodGet, "/"},
	}
	for _, rt := range routes {
		t.Run(rt.method+" "+rt.path, func(t *testing.T) {
			w := serve(t, httptest.NewRequest(rt.method, rt.path, nil))
			assert.Equal(t, http.StatusUnauthorized, w.Code,
				"unauthenticated request reached an admin route")
		})
	}
}

func TestAdminRoutesAcceptTheRightToken(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/ui/keys", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)

	w := serve(t, req)
	assert.NotEqual(t, http.StatusUnauthorized, w.Code, "a valid token must be accepted")
}

func TestAdminRoutesRejectTheWrongToken(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/ui/keys", nil)
	req.Header.Set("Authorization", "Bearer "+testToken+"-not-quite")

	w := serve(t, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

// The probe must stay public or the pod never becomes ready.
func TestHealthStaysPublic(t *testing.T) {
	w := serve(t, httptest.NewRequest(http.MethodGet, "/health", nil))
	assert.Equal(t, http.StatusOK, w.Code)
}
