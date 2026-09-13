package ui

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mathiasb/cobalt-dingo/internal/auth"
	"github.com/mathiasb/cobalt-dingo/internal/config"
	"github.com/mathiasb/cobalt-dingo/internal/domain"
)

// A Fortnox authorization covers exactly one company, so wiring a second
// entity means walking the flow again. The page hid the Connect link as soon
// as a mode had ANY company connected, which left no way to do that: the
// switcher below it only lists companies already connected.
func TestFortnoxPage_offersConnectingAnotherCompanyWhenOneIsAlreadyConnected(t *testing.T) {
	c := connectedConnector(t, map[domain.TenantID]string{
		"user-1:production:5566778899": "Definitely Mabe AB",
	})

	w := httptest.NewRecorder()
	c.pageHandler(w, requestAs("GET", "/fortnox/", sessionFor("user-1", config.ModeProduction, "5566778899")))

	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "/fortnox/connect?mode=production",
		"an already-connected mode must still offer connecting another company")
}

// Observed live on 2026-09-13: three companies in one Fortnox account share
// the organisation number 556836-0688 — two named "Definitely Mabe AB" and one
// "TEST Cobalt Dingo". The tenant key is the organisation number alone, so the
// second authorization silently replaced the first company's token and renamed
// its row. Nothing surfaced; the books simply changed underneath.
//
// Refusing is the fail-closed half of that. Distinguishing them properly needs
// an identifier Fortnox does not expose on /3/companyinformation (#87).
func TestCallback_refusesACompanySharingAnOrganisationNumberWithAConnectedOne(t *testing.T) {
	store := newConnectorTokenStore()
	repo := &recordingTenantRepo{}
	c := newTestConnector(store)
	c.tenantRepo = repo

	stubbedExchangeAndDiscovery(t, domain.Company{Name: "TEST Cobalt Dingo", OrgNumber: "556836-0688"}, nil)
	first := httptest.NewRecorder()
	c.callbackHandler(first, requestWithSession("GET", "/fortnox/callback?code=abc&state=test-oauth-nonce:production", "user-1"))
	require.Equal(t, http.StatusSeeOther, first.Code, "body: %s", first.Body.String())

	stubbedExchangeAndDiscovery(t, domain.Company{Name: "Definitely Mabe AB", OrgNumber: "556836-0688"}, nil)
	second := httptest.NewRecorder()
	c.callbackHandler(second, requestWithSession("GET", "/fortnox/callback?code=def&state=test-oauth-nonce:production", "user-1"))

	assert.Equal(t, http.StatusConflict, second.Code,
		"a different company under the same organisation number must not be stored silently")
	assert.Contains(t, second.Body.String(), "TEST Cobalt Dingo",
		"the refusal has to name the company already holding the key, or it cannot be acted on")

	require.Len(t, repo.upserted, 1, "the connected company's row must be left alone")
	assert.Equal(t, "TEST Cobalt Dingo", repo.upserted[0].Name)

	tok, err := store.Load(context.Background(), auth.TenantKey("user-1", config.ModeProduction, "5568360688"))
	require.NoError(t, err)
	assert.Equal(t, "at", tok.AccessToken, "the first company's token must survive")
}

// Re-authorizing the SAME company is the ordinary case — an expired refresh
// token, or a scope change — and must keep working.
func TestCallback_allowsReconnectingTheSameCompany(t *testing.T) {
	store := newConnectorTokenStore()
	repo := &recordingTenantRepo{}
	c := newTestConnector(store)
	c.tenantRepo = repo

	for _, code := range []string{"abc", "def"} {
		stubbedExchangeAndDiscovery(t, domain.Company{Name: "Definitely Mabe AB", OrgNumber: "556836-0688"}, nil)
		w := httptest.NewRecorder()
		c.callbackHandler(w, requestWithSession("GET", "/fortnox/callback?code="+code+"&state=test-oauth-nonce:production", "user-1"))
		require.Equal(t, http.StatusSeeOther, w.Code, "re-authorizing the same company must succeed; body: %s", w.Body.String())
	}
}
