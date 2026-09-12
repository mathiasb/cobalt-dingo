package ui

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mathiasb/cobalt-dingo/internal/config"
	"github.com/mathiasb/cobalt-dingo/internal/domain"
)

// stubDiscoveryOnly replaces just the company lookup, for tests that need to
// observe what the token exchange was called with.
func stubDiscoveryOnly(t *testing.T, company domain.Company) {
	t.Helper()
	orig := discoverCompanyFunc
	discoverCompanyFunc = func(_ context.Context, _ config.Fortnox, _ string) (domain.Company, error) {
		return company, nil
	}
	t.Cleanup(func() { discoverCompanyFunc = orig })
}

// stubbedExchangeAndDiscovery replaces both network calls in the callback and
// restores them afterwards.
func stubbedExchangeAndDiscovery(t *testing.T, company domain.Company, discoverErr error) {
	t.Helper()
	origExchange, origDiscover := exchangeFortnoxCodeFunc, discoverCompanyFunc
	exchangeFortnoxCodeFunc = func(_ context.Context, _ config.Fortnox, _ string) (domain.OAuthToken, error) {
		return domain.OAuthToken{AccessToken: "at", RefreshToken: "rt"}, nil
	}
	discoverCompanyFunc = func(_ context.Context, _ config.Fortnox, _ string) (domain.Company, error) {
		return company, discoverErr
	}
	t.Cleanup(func() {
		exchangeFortnoxCodeFunc, discoverCompanyFunc = origExchange, origDiscover
	})
}

// The defect this fixes: the callback keyed the token by "<sub>:<mode>", so
// connecting a second company overwrote the first. The company has to come
// from Fortnox, because the user cannot be asked which company the token they
// just granted belongs to — only Fortnox knows.
func TestCallback_keysTheTokenByTheCompanyFortnoxReports(t *testing.T) {
	stubbedExchangeAndDiscovery(t, domain.Company{Name: "Definitely Mabe AB", OrgNumber: "556677-8899"}, nil)
	store := newConnectorTokenStore()
	c := newTestConnector(store)

	w := httptest.NewRecorder()
	c.callbackHandler(w, requestWithSession("GET", "/fortnox/callback?code=abc&state=test-oauth-nonce:production", "user-1"))

	require.Equal(t, http.StatusSeeOther, w.Code, "body: %s", w.Body.String())
	_, err := store.Load(context.Background(), domain.TenantID("user-1:production:5566778899"))
	assert.NoError(t, err, "token must be stored under the company-scoped key")
}

// Two companies, one user, one mode: both tokens must survive.
func TestCallback_doesNotOverwriteAnotherCompany(t *testing.T) {
	store := newConnectorTokenStore()
	c := newTestConnector(store)

	stubbedExchangeAndDiscovery(t, domain.Company{Name: "First AB", OrgNumber: "556677-8899"}, nil)
	c.callbackHandler(httptest.NewRecorder(),
		requestWithSession("GET", "/fortnox/callback?code=abc&state=test-oauth-nonce:production", "user-1"))

	stubbedExchangeAndDiscovery(t, domain.Company{Name: "Second AB", OrgNumber: "112233-4455"}, nil)
	c.callbackHandler(httptest.NewRecorder(),
		requestWithSession("GET", "/fortnox/callback?code=def&state=test-oauth-nonce:production", "user-1"))

	for _, key := range []string{"user-1:production:5566778899", "user-1:production:1122334455"} {
		_, err := store.Load(context.Background(), domain.TenantID(key))
		assert.NoErrorf(t, err, "%s must still be connected", key)
	}
}

// The company name is what a human recognises, and Fortnox is the only
// authority for it. Previously the tenant row was named after the user's email
// address, which says nothing about which company it is.
func TestCallback_namesTheTenantAfterTheCompany(t *testing.T) {
	stubbedExchangeAndDiscovery(t, domain.Company{Name: "Definitely Mabe AB", OrgNumber: "556677-8899"}, nil)
	repo := &recordingTenantRepo{}
	c := newTestConnector(newConnectorTokenStore())
	c.tenantRepo = repo

	c.callbackHandler(httptest.NewRecorder(),
		requestWithSession("GET", "/fortnox/callback?code=abc&state=test-oauth-nonce:production", "user-1"))

	require.Len(t, repo.upserted, 1)
	assert.Equal(t, "Definitely Mabe AB", repo.upserted[0].Name)
	assert.Equal(t, domain.TenantID("user-1:production:5566778899"), repo.upserted[0].ID)
}

// Fail closed. A token that cannot be attributed to a company must not be
// stored: it would either overwrite an existing company's row or sit under a
// key nothing can find, and both are worse than making the user retry.
func TestCallback_storesNothingWhenTheCompanyCannotBeIdentified(t *testing.T) {
	stubbedExchangeAndDiscovery(t, domain.Company{}, errors.New("fortnox: 401 unauthorized"))
	store := newConnectorTokenStore()
	c := newTestConnector(store)

	w := httptest.NewRecorder()
	c.callbackHandler(w, requestWithSession("GET", "/fortnox/callback?code=abc&state=test-oauth-nonce:production", "user-1"))

	assert.Equal(t, http.StatusBadGateway, w.Code)
	assert.Empty(t, store.tokens, "nothing may be stored without knowing the company")
}

// An organisation number that normalises to nothing is the same problem: there
// is no key to store under.
func TestCallback_refusesACompanyWithNoUsableOrgNumber(t *testing.T) {
	stubbedExchangeAndDiscovery(t, domain.Company{Name: "No Number AB", OrgNumber: "n/a"}, nil)
	store := newConnectorTokenStore()
	c := newTestConnector(store)

	w := httptest.NewRecorder()
	c.callbackHandler(w, requestWithSession("GET", "/fortnox/callback?code=abc&state=test-oauth-nonce:production", "user-1"))

	assert.Equal(t, http.StatusBadGateway, w.Code)
	assert.Empty(t, store.tokens)
}

// recordingTenantRepo captures UpsertTenant calls so a test can assert what the
// tenant row was actually named.
type recordingTenantRepo struct {
	upserted []domain.Tenant
}

func (r *recordingTenantRepo) Get(_ context.Context, id domain.TenantID) (domain.Tenant, error) {
	for _, t := range r.upserted {
		if t.ID == id {
			return t, nil
		}
	}
	return domain.Tenant{}, errors.New("not found")
}

func (r *recordingTenantRepo) UpsertTenant(_ context.Context, t domain.Tenant) error {
	r.upserted = append(r.upserted, t)
	return nil
}

func (r *recordingTenantRepo) ListByPrefix(_ context.Context, prefix string) ([]domain.Tenant, error) {
	var out []domain.Tenant
	for _, t := range r.upserted {
		if strings.HasPrefix(string(t.ID), prefix) {
			out = append(out, t)
		}
	}
	return out, nil
}

func (r *recordingTenantRepo) DefaultDebtorAccount(_ context.Context, _ domain.TenantID) (domain.DebtorAccount, error) {
	return domain.DebtorAccount{}, errors.New("not implemented")
}
