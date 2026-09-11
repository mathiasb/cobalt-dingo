package auth

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mathiasb/cobalt-dingo/internal/config"
)

// Until now the tenant key was "<sub>:<mode>", so one user had exactly one
// Fortnox connection per mode. Connecting a second company **overwrote the
// first** — same key, same row. Mathias works with more than one company, so
// the company is now part of the key.
func TestTenantID_includesTheCompany(t *testing.T) {
	s := Session{Sub: "user-1", Mode: config.ModeProduction, Company: "5566778899"}

	tid, err := s.TenantID()

	require.NoError(t, err)
	assert.Equal(t, "user-1:production:5566778899", string(tid))
}

func TestTenantID_isDifferentPerCompany(t *testing.T) {
	a := Session{Sub: "user-1", Mode: config.ModeProduction, Company: "5566778899"}
	b := Session{Sub: "user-1", Mode: config.ModeProduction, Company: "1234567890"}

	tidA, errA := a.TenantID()
	tidB, errB := b.TenantID()

	require.NoError(t, errA)
	require.NoError(t, errB)
	assert.NotEqual(t, tidA, tidB, "two companies must not share a token row")
}

func TestTenantID_isDifferentPerMode(t *testing.T) {
	sandbox := Session{Sub: "user-1", Mode: config.ModeSandbox, Company: "5566778899"}
	production := Session{Sub: "user-1", Mode: config.ModeProduction, Company: "5566778899"}

	tidS, _ := sandbox.TenantID()
	tidP, _ := production.TenantID()

	assert.NotEqual(t, tidS, tidP, "the same company in sandbox and production are separate connections")
}

// Fail closed. A missing company must not fall back to a default or to the
// bare "<sub>:<mode>" key: either would silently operate on whichever company
// happened to be stored there, and the whole point of this key is that the
// caller cannot be vague about which company's books it is touching.
func TestTenantID_refusesWithNoCompanySelected(t *testing.T) {
	s := Session{Sub: "user-1", Mode: config.ModeProduction}

	_, err := s.TenantID()

	require.Error(t, err, "no company selected must be an error, never a default")
	assert.ErrorIs(t, err, ErrNoCompanySelected)
}

func TestTenantID_refusesWithNoMode(t *testing.T) {
	s := Session{Sub: "user-1", Company: "5566778899"}

	_, err := s.TenantID()

	require.Error(t, err)
}

func TestTenantID_refusesWithNoSubject(t *testing.T) {
	s := Session{Mode: config.ModeProduction, Company: "5566778899"}

	_, err := s.TenantID()

	require.Error(t, err)
}

// The company key comes from Fortnox's OrganizationNumber, which is formatted
// with a dash. Normalising means a re-connect cannot produce a second row for
// the same company just because the formatting differed.
func TestCompanyKey_normalisesTheOrganisationNumber(t *testing.T) {
	for _, in := range []string{"556677-8899", "5566778899", " 556677-8899 ", "556677–8899"} {
		assert.Equal(t, "5566778899", CompanyKey(in), "input %q", in)
	}
}

// An org number that normalises to nothing is not a company key. Returning ""
// here is what lets the fail-closed check above do its job.
func TestCompanyKey_rejectsSomethingWithNoDigits(t *testing.T) {
	for _, in := range []string{"", "   ", "-", "n/a"} {
		assert.Empty(t, CompanyKey(in), "input %q", in)
	}
}
