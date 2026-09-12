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

// These tests seed the token store using auth.TenantKey — the same derivation
// the production code uses — rather than a hand-written string.
//
// That is the whole point. The previous tests hand-wrote "<sub>:<mode>" keys,
// which matched the implementation because both were written together, so a
// status page that could never find a token passed its own tests.

func statusConnector(t *testing.T, owner string, mode config.Mode, companies ...string) *FortnoxConnector {
	t.Helper()
	keys := make([]domain.TenantID, 0, len(companies))
	repo := &recordingTenantRepo{}
	for _, co := range companies {
		key := auth.TenantKey(owner, mode, co)
		keys = append(keys, key)
		require.NoError(t, repo.UpsertTenant(context.Background(), domain.Tenant{ID: key, Name: "Co " + co}))
	}
	c := newTestConnector(newConnectorTokenStore(keys...))
	c.tenantRepo = repo
	c.sessions = auth.NewSessionManager("test-secret-that-is-long-enough")
	return c
}

func ownerSession(owner string, mode config.Mode, company string) *auth.Session {
	return &auth.Session{Owner: owner, Sub: "irrelevant-" + owner, Email: owner + "@d-ma.be", Mode: mode, Company: company}
}

// A mode with a connected company must report as connected. This failed before
// the fix: the probe used "<owner>:<mode>", which no stored token matches.
func TestPageHandler_reportsAModeWithAConnectedCompanyAsConnected(t *testing.T) {
	c := statusConnector(t, "owner-1", config.ModeProduction, "5566778899")

	w := httptest.NewRecorder()
	c.pageHandler(w, requestAs("GET", "/fortnox/", ownerSession("owner-1", config.ModeProduction, "5566778899")))

	require.Equal(t, http.StatusOK, w.Code)
	statuses := c.modeStatuses(context.Background(), ownerSession("owner-1", config.ModeProduction, "5566778899"))
	var production ModeStatus
	for _, s := range statuses {
		if s.Mode == config.ModeProduction {
			production = s
		}
	}
	assert.True(t, production.Connected, "a mode with a connected company must not report as disconnected")
}

func TestPageHandler_reportsAModeWithNoCompaniesAsDisconnected(t *testing.T) {
	c := statusConnector(t, "owner-1", config.ModeProduction)

	statuses := c.modeStatuses(context.Background(), ownerSession("owner-1", config.ModeProduction, ""))

	for _, s := range statuses {
		assert.False(t, s.Connected, "mode %s should be disconnected", s.Mode)
	}
}

// Disconnect must actually delete the connection. Before the fix it deleted
// "<owner>:<mode>", which no token has ever been stored under — so the button
// reported success and removed nothing.
func TestDisconnect_actuallyRemovesTheCompanysToken(t *testing.T) {
	c := statusConnector(t, "owner-1", config.ModeProduction, "5566778899")
	key := auth.TenantKey("owner-1", config.ModeProduction, "5566778899")

	_, err := c.tokenStore.Load(context.Background(), key)
	require.NoError(t, err, "fixture: the token must exist before disconnecting")

	w := httptest.NewRecorder()
	r := requestAs("POST", "/fortnox/disconnect?mode=production&company=5566778899",
		ownerSession("owner-1", config.ModeProduction, "5566778899"))
	c.disconnectHandler(w, r)

	require.Equal(t, http.StatusSeeOther, w.Code, "body: %s", w.Body.String())
	_, err = c.tokenStore.Load(context.Background(), key)
	assert.Error(t, err, "the token must be gone after disconnecting")
}

// Disconnecting must not touch another company's connection.
func TestDisconnect_leavesOtherCompaniesConnected(t *testing.T) {
	c := statusConnector(t, "owner-1", config.ModeProduction, "5566778899", "1122334455")
	keep := auth.TenantKey("owner-1", config.ModeProduction, "1122334455")

	w := httptest.NewRecorder()
	c.disconnectHandler(w, requestAs("POST", "/fortnox/disconnect?mode=production&company=5566778899",
		ownerSession("owner-1", config.ModeProduction, "5566778899")))
	require.Equal(t, http.StatusSeeOther, w.Code)

	_, err := c.tokenStore.Load(context.Background(), keep)
	assert.NoError(t, err, "the other company must still be connected")
}

func TestDisconnect_refusesWithNoCompany(t *testing.T) {
	c := statusConnector(t, "owner-1", config.ModeProduction, "5566778899")

	w := httptest.NewRecorder()
	c.disconnectHandler(w, requestAs("POST", "/fortnox/disconnect?mode=production",
		ownerSession("owner-1", config.ModeProduction, "")))

	assert.Equal(t, http.StatusBadRequest, w.Code)
}
