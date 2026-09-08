package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mathiasb/cobalt-dingo/internal/auth"
	"github.com/mathiasb/cobalt-dingo/internal/config"
)

// protectedRoute stands in for the real UI routes. It records whether it was
// ever reached, which is the only thing that distinguishes "the middleware is
// applied" from "the middleware is absent" — both return 200-shaped responses
// to a browser, which is precisely why this bug survived.
func protectedRoute(reached *bool) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /invoices", func(w http.ResponseWriter, _ *http.Request) {
		*reached = true
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	return mux
}

func enabledOIDC() config.OIDC {
	return config.OIDC{
		IssuerURL:    "https://auth.d-ma.be/application/o/cobalt-dingo/",
		ClientID:     "cobalt-dingo",
		ClientSecret: "s",
		RedirectURL:  "https://books.d-ma.be/auth/callback",
	}
}

// The regression this file exists for.
//
// auth.NewOIDCHandler performs one-shot OIDC discovery at boot. When the IdP is
// not yet serving — a co-boot race that has already happened on this estate,
// see brain wiki/homelab/failures/gitea-mcp-oidc-coboot-race-silent-jwt-degradation
// — construction fails and oidcHandler is nil. main() used to log
// "running without auth" and serve the mux unwrapped, exposing /invoices,
// /chat and the payment endpoints to anyone who could reach the ingress.
//
// Failing to build an authenticator must stop the process, not downgrade it.
func TestSecureHandler_RefusesToServeWhenOIDCIsConfiguredButUnavailable(t *testing.T) {
	var reached bool
	h, err := secureHandler(protectedRoute(&reached), auth.NewSessionManager("s"), enabledOIDC(), nil, false)

	require.ErrorIs(t, err, errNoAuthenticator,
		"OIDC is configured; failing to build it must be fatal, not a downgrade to no auth")
	assert.Nil(t, h, "no handler may be returned that could be served by accident")
	assert.False(t, reached, "the protected route must not be reachable")
}

// The same door, held open by omission rather than by failure. A dropped
// OIDC_ISSUER_URL secret must not silently produce a public deployment.
func TestSecureHandler_RefusesToServeWhenOIDCIsNotConfigured(t *testing.T) {
	var reached bool
	h, err := secureHandler(protectedRoute(&reached), auth.NewSessionManager("s"), config.OIDC{}, nil, false)

	require.ErrorIs(t, err, errNoAuthenticator)
	assert.Nil(t, h)
}

// Local development without an IdP stays possible, but only by saying so.
func TestSecureHandler_ServesUnauthenticatedOnlyWithExplicitOptIn(t *testing.T) {
	var reached bool
	h, err := secureHandler(protectedRoute(&reached), auth.NewSessionManager("s"), config.OIDC{}, nil, true)

	require.NoError(t, err)
	require.NotNil(t, h)

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/invoices", nil))
	assert.True(t, reached, "the opt-in exists so dev can run without an IdP")
}

// The opt-in is for the missing-IdP case. It must not be usable to paper over
// an IdP that is configured and broken — that is the production failure mode.
func TestSecureHandler_OptInCannotOverrideABrokenConfiguredIdP(t *testing.T) {
	var reached bool
	h, err := secureHandler(protectedRoute(&reached), auth.NewSessionManager("s"), enabledOIDC(), nil, true)

	require.ErrorIs(t, err, errNoAuthenticator,
		"COBALT_ALLOW_UNAUTHENTICATED must not disable auth that was asked for and failed")
	assert.Nil(t, h)
}

// With a working authenticator the middleware must actually be applied. Without
// this, a refactor that drops the wrapping leaves every test above green.
func TestSecureHandler_WrapsWithAuthWhenAuthenticatorPresent(t *testing.T) {
	var reached bool
	h, err := secureHandler(protectedRoute(&reached), auth.NewSessionManager("s"), enabledOIDC(), &auth.OIDCHandler{}, false)
	require.NoError(t, err)
	require.NotNil(t, h)

	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/invoices", nil))

	assert.Equal(t, http.StatusFound, w.Code, "an unauthenticated request must be redirected to login")
	assert.Equal(t, "/auth/login", w.Header().Get("Location"))
	assert.False(t, reached, "the protected route must not be reached without a session")
}

// The liveness probe must stay public, or the pod never becomes ready.
func TestSecureHandler_HealthzStaysPublic(t *testing.T) {
	var reached bool
	h, err := secureHandler(protectedRoute(&reached), auth.NewSessionManager("s"), enabledOIDC(), &auth.OIDCHandler{}, false)
	require.NoError(t, err)

	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	assert.Equal(t, http.StatusOK, w.Code)
}
