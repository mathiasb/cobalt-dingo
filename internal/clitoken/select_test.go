package clitoken_test

import (
	"testing"

	"github.com/mathiasb/cobalt-dingo/internal/clitoken"
	"github.com/mathiasb/cobalt-dingo/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func tenants() []domain.Tenant {
	return []domain.Tenant{
		{ID: "mathias:sandbox:5568360688", Name: "TEST Cobalt Dingo"},
		{ID: "mathias:production:5568360688", Name: "Definitely Mabe AB"},
	}
}

func TestSelectTenant_picksTheOneMatchingTheMode(t *testing.T) {
	got, err := clitoken.SelectTenant(tenants(), "production", "")
	require.NoError(t, err)
	assert.Equal(t, domain.TenantID("mathias:production:5568360688"), got.ID)
}

func TestSelectTenant_noneForTheModeIsAnError(t *testing.T) {
	_, err := clitoken.SelectTenant(tenants()[:1], "production", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "production")
}

// The multi-entity hazard. Two production companies may now be connected at
// once, and two of the companies on this account share organisation number
// 556836-0688 (#87), so neither the ID nor the name distinguishes them
// reliably. Picking the first match would read real books while reporting a
// mode that says nothing about which company — the exact silent-wrong shape
// AssertCompany exists to stop. Refuse and make the operator name one.
func TestSelectTenant_refusesWhenSeveralCompaniesMatchTheMode(t *testing.T) {
	several := append(tenants(), domain.Tenant{ID: "mathias:production:5560000001", Name: "Another Mabe AB"})

	_, err := clitoken.SelectTenant(several, "production", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Definitely Mabe AB")
	assert.Contains(t, err.Error(), "Another Mabe AB")
	assert.Contains(t, err.Error(), "FORTNOX_COMPANY")
}

func TestSelectTenant_anExplicitCompanyResolvesTheAmbiguity(t *testing.T) {
	several := append(tenants(), domain.Tenant{ID: "mathias:production:5560000001", Name: "Another Mabe AB"})

	got, err := clitoken.SelectTenant(several, "production", "Another Mabe AB")
	require.NoError(t, err)
	assert.Equal(t, domain.TenantID("mathias:production:5560000001"), got.ID)
}

// An explicit company that is not connected must fail, not fall back. A
// fallback here would run against a company the operator did not name.
func TestSelectTenant_explicitCompanyThatIsNotConnectedFails(t *testing.T) {
	_, err := clitoken.SelectTenant(tenants(), "production", "Some Other AB")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Some Other AB")
}

// A sandbox company must never satisfy a production request, whatever it is
// called.
func TestSelectTenant_doesNotCrossModes(t *testing.T) {
	_, err := clitoken.SelectTenant(tenants(), "production", "TEST Cobalt Dingo")
	require.Error(t, err)
}
