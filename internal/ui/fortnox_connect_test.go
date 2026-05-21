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
		store, nil, slog.Default(),
	)
}

func requestWithSession(method, target string, sub string) *http.Request {
	r := httptest.NewRequest(method, target, nil)
	sess := &auth.Session{Sub: sub, Email: sub + "@example.com", Mode: config.ModeSandbox}
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
	assert.Contains(t, w.Body.String(), "SANDBOX")
}

// TestDisconnectHandler_DeletesTokenAndRedirects verifies the happy path.
func TestDisconnectHandler_DeletesTokenAndRedirects(t *testing.T) {
	store := newConnectorTokenStore("user1:sandbox")
	c := newTestConnector(store)

	form := url.Values{"mode": {"sandbox"}}
	r := httptest.NewRequest("POST", "/fortnox/disconnect", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	sess := &auth.Session{Sub: "user1", Email: "user1@example.com", Mode: config.ModeSandbox}
	r = r.WithContext(auth.WithSession(r.Context(), sess))
	w := httptest.NewRecorder()

	c.disconnectHandler(w, r)

	assert.Equal(t, http.StatusSeeOther, w.Code)
	assert.Contains(t, w.Header().Get("Location"), "disconnected=sandbox")
	_, err := store.Load(context.Background(), "user1:sandbox")
	assert.Error(t, err, "token should be deleted")
}

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
	sess := &auth.Session{Sub: "user1", Email: "user1@example.com", Mode: config.ModeSandbox}
	r = r.WithContext(auth.WithSession(r.Context(), sess))
	w := httptest.NewRecorder()
	c.disconnectHandler(w, r)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}
