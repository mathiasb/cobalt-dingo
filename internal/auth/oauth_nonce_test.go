package auth

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewOAuthNonce_isRandomAndNonEmpty(t *testing.T) {
	a, err := NewOAuthNonce()
	require.NoError(t, err)
	b, err := NewOAuthNonce()
	require.NoError(t, err)

	assert.NotEmpty(t, a)
	assert.NotEqual(t, a, b, "two nonces must differ, or one login binds another")
	assert.GreaterOrEqual(t, len(a), 32, "a guessable nonce is not a nonce")
}

func TestOAuthNonceValid_acceptsTheMatchingNonce(t *testing.T) {
	now := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	s := Session{FortnoxNonce: "abc123", FortnoxNonceAt: now}

	assert.True(t, s.OAuthNonceValid("abc123", now.Add(30*time.Second)))
}

// The point of the whole issue: a callback carrying someone else's nonce must
// be refused, so a forged callback cannot bind a Fortnox authorization to this
// session.
func TestOAuthNonceValid_rejectsAMismatch(t *testing.T) {
	now := time.Now()
	s := Session{FortnoxNonce: "abc123", FortnoxNonceAt: now}

	assert.False(t, s.OAuthNonceValid("different", now))
}

// An empty stored nonce must not match an empty presented one. Otherwise a
// callback that simply omits the value passes, which disables the check
// without raising an error anywhere — the same trap the login flow's own
// empty-nonce test exists for.
func TestOAuthNonceValid_rejectsEmptyOnBothSides(t *testing.T) {
	now := time.Now()

	assert.False(t, Session{FortnoxNonce: "", FortnoxNonceAt: now}.OAuthNonceValid("", now))
	assert.False(t, Session{FortnoxNonce: "", FortnoxNonceAt: now}.OAuthNonceValid("abc", now))
	assert.False(t, Session{FortnoxNonce: "abc", FortnoxNonceAt: now}.OAuthNonceValid("", now))
}

// A nonce that lives as long as the 24-hour session is a 24-hour CSRF window.
// Fortnox's own authorization code expires in 10 minutes, which is the ceiling
// worth allowing.
func TestOAuthNonceValid_expires(t *testing.T) {
	issued := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	s := Session{FortnoxNonce: "abc123", FortnoxNonceAt: issued}

	assert.True(t, s.OAuthNonceValid("abc123", issued.Add(9*time.Minute)), "inside the window")
	assert.False(t, s.OAuthNonceValid("abc123", issued.Add(11*time.Minute)), "outside the window")
}

// A nonce with no issue time cannot be aged, so it must not be trusted —
// otherwise an old session with a nonce and no timestamp is valid forever.
func TestOAuthNonceValid_rejectsAnUnstampedNonce(t *testing.T) {
	assert.False(t, Session{FortnoxNonce: "abc123"}.OAuthNonceValid("abc123", time.Now()))
}

// Clock skew backwards must not extend the window either.
func TestOAuthNonceValid_rejectsANonceIssuedInTheFuture(t *testing.T) {
	now := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	s := Session{FortnoxNonce: "abc123", FortnoxNonceAt: now.Add(time.Hour)}

	assert.False(t, s.OAuthNonceValid("abc123", now))
}

// state has to carry the mode as well, because the callback routes on it.
func TestOAuthState_roundTrips(t *testing.T) {
	state := OAuthState("the-nonce", "production")

	nonce, mode, ok := ParseOAuthState(state)

	require.True(t, ok)
	assert.Equal(t, "the-nonce", nonce)
	assert.Equal(t, "production", mode)
}

func TestParseOAuthState_rejectsMalformedInput(t *testing.T) {
	for _, in := range []string{"", "onlyonepart", ":", "nonce:", ":production", "a:b:c"} {
		_, _, ok := ParseOAuthState(in)
		assert.Falsef(t, ok, "input %q must not parse", in)
	}
}
