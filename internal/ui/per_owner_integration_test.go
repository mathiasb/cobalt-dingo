package ui

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mathiasb/cobalt-dingo/internal/config"
	"github.com/mathiasb/cobalt-dingo/internal/crypto"
	"github.com/mathiasb/cobalt-dingo/internal/domain"
)

type ownerStore struct {
	rec domain.Integration
	err error
}

func (o ownerStore) Get(context.Context, string, string) (domain.Integration, error) {
	return o.rec, o.err
}

const ownerTestSecret = "Kq7#vP2mZx9!tR4wLs8@nB6yHj3^dF5gCe1%uA0oXi7*qW2zTv4rMk9$bN6pJ8hY"

// The point of ADR-0005: the authorize URL must carry the TENANT's client id,
// not the application-level one. Getting this wrong sends the tenant to
// authorize the operator's integration, which looks like it works.
func TestConnect_usesTheOwnersOwnClientID(t *testing.T) {
	cipher, err := crypto.NewCipher(ownerTestSecret)
	require.NoError(t, err)
	sealed, err := cipher.Seal("tenant-secret")
	require.NoError(t, err)

	c := newTestConnector(newConnectorTokenStore())
	c.integrations = ownerStore{rec: domain.Integration{ClientID: "tenant-client-id", ClientSecretSealed: sealed}}
	c.cipher = cipher

	w := httptest.NewRecorder()
	c.connectHandler(w, requestWithSession("GET", "/fortnox/connect?mode=production", "user-1"))

	require.Equal(t, http.StatusFound, w.Code, "body: %s", w.Body.String())
	assert.Contains(t, w.Header().Get("Location"), "client_id=tenant-client-id")
	assert.NotContains(t, w.Header().Get("Location"), "client_id=p",
		"the application-level client id must not be used when the owner has their own")
}

// An owner with no registered integration falls back to the application-level
// one — the operator's own case, and any tenant who has not registered.
func TestConnect_fallsBackToTheApplicationClientID(t *testing.T) {
	c := newTestConnector(newConnectorTokenStore())
	c.integrations = ownerStore{err: domain.ErrNoIntegration}

	w := httptest.NewRecorder()
	c.connectHandler(w, requestWithSession("GET", "/fortnox/connect?mode=production", "user-1"))

	require.Equal(t, http.StatusFound, w.Code)
	assert.Contains(t, w.Header().Get("Location"), "client_id=p")
}

// Fail closed rather than silently sending the tenant to authorize OUR
// integration, which would bind their company to the operator's app.
func TestConnect_refusesWhenTheOwnersIntegrationCannotBeResolved(t *testing.T) {
	c := newTestConnector(newConnectorTokenStore())
	c.integrations = ownerStore{rec: domain.Integration{ClientID: "tenant-client-id", ClientSecretSealed: "undecryptable"}}
	cipher, err := crypto.NewCipher(ownerTestSecret)
	require.NoError(t, err)
	c.cipher = cipher

	w := httptest.NewRecorder()
	c.connectHandler(w, requestWithSession("GET", "/fortnox/connect?mode=production", "user-1"))

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.NotContains(t, w.Header().Get("Location"), "client_id")
}

// With no integration store at all — single-tenant, no database — the
// application-level credentials are used and nothing errors.
func TestConnect_worksWithNoIntegrationStore(t *testing.T) {
	c := newTestConnector(newConnectorTokenStore())

	w := httptest.NewRecorder()
	c.connectHandler(w, requestWithSession("GET", "/fortnox/connect?mode=sandbox", "user-1"))

	require.Equal(t, http.StatusFound, w.Code)
	assert.Contains(t, w.Header().Get("Location"), "client_id=s")
}

// The token exchange must use the same credentials the authorize step did.
// Using the owner's client id to authorize and ours to exchange fails at
// Fortnox with an error that names neither.
func TestCallback_exchangesWithTheOwnersCredentials(t *testing.T) {
	cipher, err := crypto.NewCipher(ownerTestSecret)
	require.NoError(t, err)
	sealed, err := cipher.Seal("tenant-secret")
	require.NoError(t, err)

	var usedClientID, usedSecret string
	origExchange := exchangeFortnoxCodeFunc
	exchangeFortnoxCodeFunc = func(_ context.Context, cfg config.Fortnox, _ string) (domain.OAuthToken, error) {
		usedClientID, usedSecret = cfg.ClientID, cfg.ClientSecret
		return domain.OAuthToken{AccessToken: "at"}, nil
	}
	t.Cleanup(func() { exchangeFortnoxCodeFunc = origExchange })
	stubDiscoveryOnly(t, domain.Company{Name: "Tenant AB", OrgNumber: "556677-8899"})

	c := newTestConnector(newConnectorTokenStore())
	c.integrations = ownerStore{rec: domain.Integration{ClientID: "tenant-client-id", ClientSecretSealed: sealed}}
	c.cipher = cipher

	c.callbackHandler(httptest.NewRecorder(),
		requestWithSession("GET", "/fortnox/callback?code=abc&state=production", "user-1"))

	assert.Equal(t, "tenant-client-id", usedClientID)
	assert.Equal(t, "tenant-secret", usedSecret)
}
