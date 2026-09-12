package ui

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/mathiasb/cobalt-dingo/internal/auth"
	"github.com/mathiasb/cobalt-dingo/internal/config"
	"github.com/mathiasb/cobalt-dingo/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// connectorTokenStore is a test double for domain.TokenStore used in connector tests.
type connectorTokenStore struct {
	tokens map[domain.TenantID]domain.OAuthToken
}

func newConnectorTokenStore(connected ...domain.TenantID) *connectorTokenStore {
	s := &connectorTokenStore{tokens: map[domain.TenantID]domain.OAuthToken{}}
	for _, tid := range connected {
		s.tokens[tid] = domain.OAuthToken{AccessToken: "tok"}
	}
	return s
}

func (s *connectorTokenStore) Load(_ context.Context, tid domain.TenantID) (domain.OAuthToken, error) {
	tok, ok := s.tokens[tid]
	if !ok {
		return domain.OAuthToken{}, fmt.Errorf("no token for %s", tid)
	}
	return tok, nil
}

func (s *connectorTokenStore) Save(_ context.Context, tid domain.TenantID, tok domain.OAuthToken) error {
	s.tokens[tid] = tok
	return nil
}

func (s *connectorTokenStore) AtomicRefresh(_ context.Context, _ domain.TenantID, _, _ domain.OAuthToken) error {
	return nil
}

func (s *connectorTokenStore) Delete(_ context.Context, tid domain.TenantID) error {
	delete(s.tokens, tid)
	return nil
}

func newTestConnector(store domain.TokenStore) *FortnoxConnector {
	return NewFortnoxConnector(
		map[config.Mode]config.Fortnox{
			config.ModeSandbox:    {ClientID: "s"},
			config.ModeProduction: {ClientID: "p"},
		},
		store, nil, auth.NewSessionManager("test-secret-that-is-long-enough"), nil, nil, slog.Default(),
	)
}

func requestWithSession(method, target string, sub string) *http.Request {
	r := httptest.NewRequest(method, target, nil)
	sess := &auth.Session{Owner: sub, Sub: "sub-" + sub, Email: sub + "@example.com", Mode: config.ModeSandbox}
	return r.WithContext(auth.WithSession(r.Context(), sess))
}

// TestPageHandler_ShowsBothModes verifies both sandbox and production cards render.
func TestPageHandler_ShowsBothModes(t *testing.T) {
	store := newConnectorTokenStore("user1:sandbox")
	c := newTestConnector(store)

	r := requestWithSession("GET", "/fortnox/", "user1")
	w := httptest.NewRecorder()
	c.pageHandler(w, r)

	require.Equal(t, http.StatusOK, w.Code)
	body := w.Body.String()
	assert.Contains(t, body, "Sandbox")
	assert.Contains(t, body, "Production")
}

// TestPageHandler_RequiresAuth verifies unauthenticated requests get 401.
func TestPageHandler_RequiresAuth(t *testing.T) {
	c := newTestConnector(newConnectorTokenStore())
	r := httptest.NewRequest("GET", "/fortnox/", nil)
	w := httptest.NewRecorder()
	c.pageHandler(w, r)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

// TestPageHandler_FlashMessage verifies ?connected=sandbox shows flash text.
func TestPageHandler_FlashMessage(t *testing.T) {
	c := newTestConnector(newConnectorTokenStore())
	r := requestWithSession("GET", "/fortnox/?connected=sandbox", "user1")
	w := httptest.NewRecorder()
	c.pageHandler(w, r)

	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "Successfully connected to")
}

// The happy path moved to TestDisconnect_actuallyRemovesTheCompanysToken in
// connection_status_test.go, which seeds the store via auth.TenantKey.
//
// The version that used to live here seeded "user1:sandbox" by hand and passed
// no company. It agreed with the implementation because both were written
// together — and both were wrong: disconnect deleted a two-part key no token
// has ever been stored under, so the button reported success and removed
// nothing. A test that hand-writes the key it expects cannot catch a key-format
// bug, because it IS the bug, asserted.

// TestDisconnectHandler_RequiresAuth verifies unauthenticated requests get 401.
func TestDisconnectHandler_RequiresAuth(t *testing.T) {
	c := newTestConnector(newConnectorTokenStore())
	r := httptest.NewRequest("POST", "/fortnox/disconnect", nil)
	w := httptest.NewRecorder()
	c.disconnectHandler(w, r)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

// TestDisconnectHandler_InvalidMode verifies bad mode param gets 400.
func TestDisconnectHandler_InvalidMode(t *testing.T) {
	c := newTestConnector(newConnectorTokenStore())
	form := url.Values{"mode": {"bogus"}}
	r := httptest.NewRequest("POST", "/fortnox/disconnect", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	sess := &auth.Session{Owner: "user1", Sub: "sub-user1", Email: "user1@example.com", Mode: config.ModeSandbox}
	r = r.WithContext(auth.WithSession(r.Context(), sess))
	w := httptest.NewRecorder()
	c.disconnectHandler(w, r)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// TestCallbackHandler_ExchangesCodeAndRedirects verifies successful OAuth callback.
func TestCallbackHandler_ExchangesCodeAndRedirects(t *testing.T) {
	old := exchangeFortnoxCodeFunc
	exchangeFortnoxCodeFunc = func(_ context.Context, _ config.Fortnox, _ string) (domain.OAuthToken, error) {
		return domain.OAuthToken{AccessToken: "atok", RefreshToken: "rtok"}, nil
	}
	defer func() { exchangeFortnoxCodeFunc = old }()

	// The callback now also asks Fortnox which company the token belongs to, so
	// that call needs stubbing too. Without this the test made a real network
	// request — which is how it first failed, in 0.10s of DNS.
	stubbedExchangeAndDiscovery(t, domain.Company{Name: "Sandbox AB", OrgNumber: "556677-8899"}, nil)

	store := newConnectorTokenStore()
	c := newTestConnector(store)

	r := requestWithSession("GET", "/fortnox/callback?code=abc123&state=sandbox", "user1")
	w := httptest.NewRecorder()

	c.callbackHandler(w, r)

	assert.Equal(t, http.StatusSeeOther, w.Code)
	assert.Contains(t, w.Header().Get("Location"), "connected=sandbox")

	tok, err := store.Load(context.Background(), "user1:sandbox:5566778899")
	require.NoError(t, err)
	assert.Equal(t, "at", tok.AccessToken, "the stub from stubbedExchangeAndDiscovery wins")
}
