package ui

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mathiasb/cobalt-dingo/internal/auth"
	"github.com/mathiasb/cobalt-dingo/internal/config"
	"github.com/mathiasb/cobalt-dingo/internal/domain"
)

// connectedConnector wires a connector whose store holds tokens for the given
// tenant keys and whose tenant repo knows their company names.
func connectedConnector(t *testing.T, named map[domain.TenantID]string) *FortnoxConnector {
	t.Helper()
	keys := make([]domain.TenantID, 0, len(named))
	repo := &recordingTenantRepo{}
	for tid, name := range named {
		keys = append(keys, tid)
		require.NoError(t, repo.UpsertTenant(context.Background(), domain.Tenant{ID: tid, Name: name}))
	}
	c := newTestConnector(newConnectorTokenStore(keys...))
	c.tenantRepo = repo
	c.sessions = auth.NewSessionManager("test-secret-that-is-long-enough")
	return c
}

func sessionFor(sub string, mode config.Mode, company string) *auth.Session {
	return &auth.Session{Owner: sub, Sub: "sub-" + sub, Email: sub + "@example.com", Mode: mode, Company: company}
}

func requestAs(method, target string, sess *auth.Session) *http.Request {
	r := httptest.NewRequest(method, target, nil)
	return r.WithContext(auth.WithSession(r.Context(), sess))
}

// The page has to show which companies are connected and which one is active,
// or "switch between them" has nothing to switch between.
func TestCompaniesPage_listsConnectedCompaniesForTheActiveMode(t *testing.T) {
	c := connectedConnector(t, map[domain.TenantID]string{
		"user-1:production:5566778899": "Definitely Mabe AB",
		"user-1:production:1122334455": "Second Company AB",
		"user-1:sandbox:9988776655":    "Sandbox Only AB",
	})

	w := httptest.NewRecorder()
	c.pageHandler(w, requestAs("GET", "/fortnox/", sessionFor("user-1", config.ModeProduction, "5566778899")))

	require.Equal(t, http.StatusOK, w.Code)
	body := w.Body.String()
	assert.Contains(t, body, "Definitely Mabe AB")
	assert.Contains(t, body, "Second Company AB")
	assert.NotContains(t, body, "Sandbox Only AB",
		"a sandbox connection is not a production company — showing it invites switching into the wrong books")
}

// Selecting a company writes it to the signed session cookie, which is what
// makes the choice survive the next request.
func TestSelectCompany_setsTheSessionCookie(t *testing.T) {
	c := connectedConnector(t, map[domain.TenantID]string{
		"user-1:production:5566778899": "Definitely Mabe AB",
		"user-1:production:1122334455": "Second Company AB",
	})

	w := httptest.NewRecorder()
	r := requestAs("POST", "/fortnox/company?company=1122334455", sessionFor("user-1", config.ModeProduction, "5566778899"))
	c.selectCompanyHandler(w, r)

	require.Equal(t, http.StatusSeeOther, w.Code)
	cookie := w.Header().Get("Set-Cookie")
	require.NotEmpty(t, cookie, "the selection must be persisted in the session cookie")
	assert.Contains(t, cookie, "HttpOnly", "the session cookie keeps its protective flags")
}

// Fail closed: you may only work with a company you have actually connected.
// Without this check a crafted form value would select any company key, and the
// app would then try to operate on books it has no token for.
func TestSelectCompany_refusesACompanyWithNoToken(t *testing.T) {
	c := connectedConnector(t, map[domain.TenantID]string{
		"user-1:production:5566778899": "Definitely Mabe AB",
	})

	w := httptest.NewRecorder()
	r := requestAs("POST", "/fortnox/company?company=9999999999", sessionFor("user-1", config.ModeProduction, "5566778899"))
	c.selectCompanyHandler(w, r)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Empty(t, w.Header().Get("Set-Cookie"), "nothing may be selected without a token for it")
}

func TestSelectCompany_requiresAuth(t *testing.T) {
	c := connectedConnector(t, map[domain.TenantID]string{})

	w := httptest.NewRecorder()
	c.selectCompanyHandler(w, httptest.NewRequest("POST", "/fortnox/company?company=5566778899", nil))

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestSelectCompany_rejectsAnEmptyCompany(t *testing.T) {
	c := connectedConnector(t, map[domain.TenantID]string{
		"user-1:production:5566778899": "Definitely Mabe AB",
	})

	w := httptest.NewRecorder()
	r := requestAs("POST", "/fortnox/company?company=", sessionFor("user-1", config.ModeProduction, "5566778899"))
	c.selectCompanyHandler(w, r)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// A company key is normalised on the way in, so a dashed organisation number
// selects the same company rather than silently failing the token check.
func TestSelectCompany_normalisesTheSubmittedOrgNumber(t *testing.T) {
	c := connectedConnector(t, map[domain.TenantID]string{
		"user-1:production:5566778899": "Definitely Mabe AB",
	})

	w := httptest.NewRecorder()
	r := requestAs("POST", "/fortnox/company?company=556677-8899", sessionFor("user-1", config.ModeProduction, ""))
	c.selectCompanyHandler(w, r)

	assert.Equal(t, http.StatusSeeOther, w.Code)
	assert.NotEmpty(t, w.Header().Get("Set-Cookie"))
}

// The page must not blow up before any company is connected — that is the
// state every new user starts in.
func TestCompaniesPage_worksWithNothingConnected(t *testing.T) {
	c := connectedConnector(t, map[domain.TenantID]string{})

	w := httptest.NewRecorder()
	c.pageHandler(w, requestAs("GET", "/fortnox/", sessionFor("user-1", config.ModeProduction, "")))

	require.Equal(t, http.StatusOK, w.Code)
	assert.True(t, strings.Contains(w.Body.String(), "Fortnox"), "the page still renders")
}
