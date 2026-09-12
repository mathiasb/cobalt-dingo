package auth

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mathiasb/cobalt-dingo/internal/config"
)

// The tenant key is "<owner>:<mode>:<company>".
//
// Two separate defects shaped it. It was "<sub>:<mode>", so one user had
// exactly one connection per mode and a second company overwrote the first;
// and it derived from the OIDC subject, which orphaned a live token when the
// subject changed (ADR-0003).
func TestTenantID_includesTheCompany(t *testing.T) {
	s := Session{Owner: "user-1", Mode: config.ModeProduction, Company: "5566778899"}

	tid, err := s.TenantID()

	require.NoError(t, err)
	assert.Equal(t, "user-1:production:5566778899", string(tid))
}

func TestTenantID_isDifferentPerCompany(t *testing.T) {
	a := Session{Owner: "user-1", Mode: config.ModeProduction, Company: "5566778899"}
	b := Session{Owner: "user-1", Mode: config.ModeProduction, Company: "1234567890"}

	tidA, errA := a.TenantID()
	tidB, errB := b.TenantID()

	require.NoError(t, errA)
	require.NoError(t, errB)
	assert.NotEqual(t, tidA, tidB, "two companies must not share a token row")
}

func TestTenantID_isDifferentPerMode(t *testing.T) {
	sandbox := Session{Owner: "user-1", Mode: config.ModeSandbox, Company: "5566778899"}
	production := Session{Owner: "user-1", Mode: config.ModeProduction, Company: "5566778899"}

	tidS, _ := sandbox.TenantID()
	tidP, _ := production.TenantID()

	assert.NotEqual(t, tidS, tidP, "the same company in sandbox and production are separate connections")
}

// Fail closed. A missing company must not fall back to a default or to the
// bare "<sub>:<mode>" key: either would silently operate on whichever company
// happened to be stored there, and the whole point of this key is that the
// caller cannot be vague about which company's books it is touching.
func TestTenantID_refusesWithNoCompanySelected(t *testing.T) {
	s := Session{Owner: "user-1", Mode: config.ModeProduction}

	_, err := s.TenantID()

	require.Error(t, err, "no company selected must be an error, never a default")
	assert.ErrorIs(t, err, ErrNoCompanySelected)
}

func TestTenantID_refusesWithNoMode(t *testing.T) {
	s := Session{Owner: "user-1", Company: "5566778899"}

	_, err := s.TenantID()

	require.Error(t, err)
}

// A subject is NOT required to derive the key any more — that is the point of
// ADR-0003. A session with an owner and no subject is unusual but perfectly
// keyable, and asserting otherwise would re-couple the key to the subject.
// The no-owner case is TestTenantID_refusesWithNoOwner.
func TestTenantID_doesNotNeedTheSubject(t *testing.T) {
	s := Session{Owner: "user-1", Mode: config.ModeProduction, Company: "5566778899"}

	tid, err := s.TenantID()

	require.NoError(t, err)
	assert.Equal(t, "user-1:production:5566778899", string(tid))
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
