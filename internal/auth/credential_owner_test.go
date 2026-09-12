package auth

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mathiasb/cobalt-dingo/internal/config"
	"github.com/mathiasb/cobalt-dingo/internal/domain"
)

// fakeDirectory mints an owner id per email, exactly as a real directory must:
// the same email always resolves to the same id, whatever subject presented it.
type fakeDirectory struct {
	byEmail map[string]string
	seen    []string // subjects recorded, in order
	err     error
}

func newFakeDirectory() *fakeDirectory {
	return &fakeDirectory{byEmail: map[string]string{}}
}

func (f *fakeDirectory) ResolveOwner(_ context.Context, email, sub string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	f.seen = append(f.seen, sub)
	if id, ok := f.byEmail[email]; ok {
		return id, nil
	}
	id := "owner-" + email
	f.byEmail[email] = id
	return id, nil
}

// THE test ADR-0003 and #67 both name as the definition of done, and the
// Dex→Authentik incident reproduced.
//
// That migration changed `sub` for the same human. Login, discovery, callback
// and middleware were all green; /invoices then reported
// `load token: no token for tenant <64-hex>:sandbox`. The credential was not
// deleted — it was orphaned under a key nobody would ever compute again.
func TestCredentialOwner_SurvivesSubjectChange(t *testing.T) {
	dir := newFakeDirectory()
	const email = "mathias@d-ma.be"

	// Before: the IdP issues one subject.
	before, err := NewSession(context.Background(), dir, verifiedIdentity{
		Sub: "dex-subject-aaaa", Email: email, EmailVerified: true,
	}, config.ModeSandbox)
	require.NoError(t, err)
	before.Company = "5566778899"

	keyBefore, err := before.TenantID()
	require.NoError(t, err)

	// A token is stored under that key.
	tokens := map[domain.TenantID]string{keyBefore: "the-refresh-token"}

	// After: a provider change hands the same human a completely different
	// subject. Nothing else about them changed.
	after, err := NewSession(context.Background(), dir, verifiedIdentity{
		Sub: "authentik-subject-64-hex-bbbb", Email: email, EmailVerified: true,
	}, config.ModeSandbox)
	require.NoError(t, err)
	after.Company = "5566778899"

	keyAfter, err := after.TenantID()
	require.NoError(t, err)

	assert.Equal(t, keyBefore, keyAfter,
		"the same verified email must resolve to the same credential owner across a subject change")
	assert.Equal(t, "the-refresh-token", tokens[keyAfter],
		"the stored token must still be reachable — this is the orphaning the ADR exists to prevent")

	// Both subjects are recorded, because they are audit data even though they
	// are not the key.
	assert.Equal(t, []string{"dex-subject-aaaa", "authentik-subject-64-hex-bbbb"}, dir.seen)
}

// Different humans must not collide, which is the other half of the property.
func TestCredentialOwner_differentEmailsGetDifferentOwners(t *testing.T) {
	dir := newFakeDirectory()

	a, err := NewSession(context.Background(), dir, verifiedIdentity{Sub: "s1", Email: "a@d-ma.be", EmailVerified: true}, config.ModeSandbox)
	require.NoError(t, err)
	b, err := NewSession(context.Background(), dir, verifiedIdentity{Sub: "s2", Email: "b@d-ma.be", EmailVerified: true}, config.ModeSandbox)
	require.NoError(t, err)

	a.Company, b.Company = "1111111111", "1111111111"
	keyA, err := a.TenantID()
	require.NoError(t, err)
	keyB, err := b.TenantID()
	require.NoError(t, err)

	assert.NotEqual(t, keyA, keyB)
}

// Mode stays in the key. ADR-0003 keeps this deliberately: sandbox and
// production credentials must never resolve to each other.
func TestCredentialOwner_modeStillSeparatesCredentials(t *testing.T) {
	dir := newFakeDirectory()
	const email = "mathias@d-ma.be"

	sandbox, err := NewSession(context.Background(), dir, verifiedIdentity{Sub: "s", Email: email, EmailVerified: true}, config.ModeSandbox)
	require.NoError(t, err)
	production, err := NewSession(context.Background(), dir, verifiedIdentity{Sub: "s", Email: email, EmailVerified: true}, config.ModeProduction)
	require.NoError(t, err)

	sandbox.Company, production.Company = "5566778899", "5566778899"
	keyS, _ := sandbox.TenantID()
	keyP, _ := production.TenantID()

	assert.NotEqual(t, keyS, keyP)
}

// ADR-0003's premise: keying on an UNVERIFIED email is worse than keying on
// sub, because it is attacker-influenceable rather than merely unstable. So an
// unverified email must not produce a session at all.
func TestNewSession_refusesAnUnverifiedEmail(t *testing.T) {
	dir := newFakeDirectory()

	_, err := NewSession(context.Background(), dir, verifiedIdentity{
		Sub: "s", Email: "mathias@d-ma.be", EmailVerified: false,
	}, config.ModeSandbox)

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrEmailNotVerified)
}

func TestNewSession_refusesAnEmptyEmail(t *testing.T) {
	dir := newFakeDirectory()

	_, err := NewSession(context.Background(), dir, verifiedIdentity{Sub: "s", Email: ""}, config.ModeSandbox)

	require.Error(t, err)
}

// The key must not silently become "":mode:company if the directory fails.
func TestNewSession_refusesWhenTheDirectoryFails(t *testing.T) {
	dir := newFakeDirectory()
	dir.err = assert.AnError

	_, err := NewSession(context.Background(), dir, verifiedIdentity{Sub: "s", Email: "a@d-ma.be", EmailVerified: true}, config.ModeSandbox)

	require.Error(t, err)
}

// A session with no owner must never produce a credential key.
func TestTenantID_refusesWithNoOwner(t *testing.T) {
	s := Session{Mode: config.ModeSandbox, Company: "5566778899"}

	_, err := s.TenantID()

	require.Error(t, err)
}
