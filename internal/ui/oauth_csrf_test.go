package ui

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mathiasb/cobalt-dingo/internal/auth"
	"github.com/mathiasb/cobalt-dingo/internal/config"
	"github.com/mathiasb/cobalt-dingo/internal/domain"
)

// csrfConnector has a session manager, so connect can persist the nonce it
// issues.
func csrfConnector(t *testing.T) *FortnoxConnector {
	t.Helper()
	c := newTestConnector(newConnectorTokenStore())
	c.sessions = auth.NewSessionManager("test-secret-that-is-long-enough")
	c.tenantRepo = &recordingTenantRepo{}
	return c
}

// connect must issue a nonce, persist it in the session, and put it in state.
func TestConnect_issuesANonceAndPutsItInState(t *testing.T) {
	c := csrfConnector(t)
	sess := ownerSession("owner-1", config.ModeProduction, "")

	w := httptest.NewRecorder()
	c.connectHandler(w, requestAs("GET", "/fortnox/connect?mode=production", sess))

	require.Equal(t, http.StatusFound, w.Code, "body: %s", w.Body.String())
	require.NotEmpty(t, w.Header().Get("Set-Cookie"), "the nonce must be persisted in the session")

	loc, err := url.Parse(w.Header().Get("Location"))
	require.NoError(t, err)
	nonce, mode, ok := auth.ParseOAuthState(loc.Query().Get("state"))
	require.True(t, ok, "state must parse as <nonce>:<mode>, got %q", loc.Query().Get("state"))
	assert.Equal(t, "production", mode)
	assert.NotEmpty(t, nonce)
	assert.NotEqual(t, "production", nonce, "the mode must not be doubling as the nonce")
}

// THE defect. A callback whose state carries a nonce this session never issued
// must be refused, so a forged callback cannot bind a Fortnox authorization.
func TestCallback_refusesANonceThisSessionNeverIssued(t *testing.T) {
	c := csrfConnector(t)
	stubbedExchangeAndDiscovery(t, domain.Company{Name: "Attacker AB", OrgNumber: "556677-8899"}, nil)
	sess := ownerSession("owner-1", config.ModeProduction, "")
	sess.FortnoxNonce = "the-nonce-we-issued"
	sess.FortnoxNonceAt = time.Now()

	w := httptest.NewRecorder()
	c.callbackHandler(w, requestAs("GET",
		"/fortnox/callback?code=abc&state="+auth.OAuthState("a-nonce-from-somewhere-else", "production"), sess))

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Empty(t, c.tokenStore.(*connectorTokenStore).tokens, "nothing may be stored")
}

// A callback with no nonce in the session cannot be validated, so it must fail
// closed rather than skip the comparison.
func TestCallback_refusesWhenTheSessionHasNoNonce(t *testing.T) {
	c := csrfConnector(t)
	stubbedExchangeAndDiscovery(t, domain.Company{Name: "Co", OrgNumber: "556677-8899"}, nil)
	sess := ownerSession("owner-1", config.ModeProduction, "")

	w := httptest.NewRecorder()
	c.callbackHandler(w, requestAs("GET",
		"/fortnox/callback?code=abc&state="+auth.OAuthState("anything", "production"), sess))

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// The old format — state being just the mode — must no longer be accepted.
// Leaving it accepted "for compatibility" would leave the hole open.
func TestCallback_refusesTheOldModeOnlyState(t *testing.T) {
	c := csrfConnector(t)
	stubbedExchangeAndDiscovery(t, domain.Company{Name: "Co", OrgNumber: "556677-8899"}, nil)
	sess := ownerSession("owner-1", config.ModeProduction, "")
	sess.FortnoxNonce = "n"
	sess.FortnoxNonceAt = time.Now()

	w := httptest.NewRecorder()
	c.callbackHandler(w, requestAs("GET", "/fortnox/callback?code=abc&state=production", sess))

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// An expired nonce is refused: the window is bounded by Fortnox's own
// ten-minute authorization code lifetime, not by the 24-hour session.
func TestCallback_refusesAnExpiredNonce(t *testing.T) {
	c := csrfConnector(t)
	stubbedExchangeAndDiscovery(t, domain.Company{Name: "Co", OrgNumber: "556677-8899"}, nil)
	sess := ownerSession("owner-1", config.ModeProduction, "")
	sess.FortnoxNonce = "n"
	sess.FortnoxNonceAt = time.Now().Add(-11 * time.Minute)

	w := httptest.NewRecorder()
	c.callbackHandler(w, requestAs("GET",
		"/fortnox/callback?code=abc&state="+auth.OAuthState("n", "production"), sess))

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// The happy path still completes, and the nonce is CONSUMED: the session cookie
// the callback writes back no longer carries it, so a genuine replay — the same
// callback with the cookie the browser now holds — fails.
//
// Asserted on the re-issued cookie rather than on in-memory mutation, because
// the cookie is what a real second request would present.
func TestCallback_acceptsTheMatchingNonceAndConsumesIt(t *testing.T) {
	c := csrfConnector(t)
	stubbedExchangeAndDiscovery(t, domain.Company{Name: "Real AB", OrgNumber: "556677-8899"}, nil)
	sess := ownerSession("owner-1", config.ModeProduction, "")
	sess.FortnoxNonce = "matching-nonce"
	sess.FortnoxNonceAt = time.Now()
	state := auth.OAuthState("matching-nonce", "production")

	w := httptest.NewRecorder()
	c.callbackHandler(w, requestAs("GET", "/fortnox/callback?code=abc&state="+state, sess))
	require.Equal(t, http.StatusSeeOther, w.Code, "body: %s", w.Body.String())

	// Read back the session the callback issued.
	next := httptest.NewRequest("GET", "/fortnox/", nil)
	for _, ck := range w.Result().Cookies() {
		next.AddCookie(ck)
	}
	stored := c.sessions.Get(next)
	require.NotNil(t, stored, "the callback must re-issue a readable session")
	assert.Empty(t, stored.FortnoxNonce, "the nonce must be consumed")
	assert.Equal(t, "5566778899", stored.Company,
		"connecting a company is also what starts working with it")
}
