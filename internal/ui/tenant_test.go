package ui

import (
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mathiasb/cobalt-dingo/internal/auth"
	"github.com/mathiasb/cobalt-dingo/internal/config"
)

func TestTenantID_resolvesFromTheSessionsSelectedCompany(t *testing.T) {
	srv := &Server{}
	r := httptest.NewRequest("GET", "/invoices", nil)
	r = r.WithContext(auth.WithSession(r.Context(), &auth.Session{
		Sub: "user-1", Mode: config.ModeProduction, Company: "556677-8899",
	}))

	tid, err := srv.tenantID(r)

	require.NoError(t, err)
	assert.Equal(t, "user-1:production:5566778899", string(tid))
}

// This resolver used to return domain.TenantID("default") whenever there was no
// session — a fail-open that served *some* tenant's data to an unauthenticated
// request. With a company dimension it would serve a specific real company's
// books, so the fallback is gone.
func TestTenantID_refusesWithNoSession(t *testing.T) {
	srv := &Server{}

	_, err := srv.tenantID(httptest.NewRequest("GET", "/invoices", nil))

	require.Error(t, err, `no session must not resolve to "default"`)
}

func TestTenantID_refusesWhenNoCompanyIsSelected(t *testing.T) {
	srv := &Server{}
	r := httptest.NewRequest("GET", "/invoices", nil)
	r = r.WithContext(auth.WithSession(r.Context(), &auth.Session{
		Sub: "user-1", Mode: config.ModeProduction,
	}))

	_, err := srv.tenantID(r)

	require.ErrorIs(t, err, auth.ErrNoCompanySelected)
}
