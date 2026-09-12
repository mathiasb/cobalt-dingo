package crypto_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mathiasb/cobalt-dingo/internal/crypto"
)

// A fingerprint has to be DETERMINISTIC, which is the whole reason it exists:
// GCM ciphertext is randomised, so `WHERE refresh_token = $1` cannot work once
// the column is encrypted. The compare-and-swap that detects the rolling-refresh
// race matches on this instead.
func TestFingerprint_isDeterministic(t *testing.T) {
	c, err := crypto.NewCipher(testKey)
	require.NoError(t, err)

	assert.Equal(t, c.Fingerprint("the-refresh-token"), c.Fingerprint("the-refresh-token"))
}

func TestFingerprint_differsPerInput(t *testing.T) {
	c, err := crypto.NewCipher(testKey)
	require.NoError(t, err)

	assert.NotEqual(t, c.Fingerprint("token-a"), c.Fingerprint("token-b"))
}

// Keyed, not a bare hash: a database dump must not let someone confirm a
// guessed token by hashing it.
func TestFingerprint_isKeyed(t *testing.T) {
	a, err := crypto.NewCipher(testKey)
	require.NoError(t, err)
	b, err := crypto.NewCipher(otherKey)
	require.NoError(t, err)

	assert.NotEqual(t, a.Fingerprint("same-token"), b.Fingerprint("same-token"),
		"two deployments with different keys must not produce the same fingerprint")
}

// The fingerprint must not be derived with the same key as the encryption, or
// the two uses of one secret are not separated.
func TestFingerprint_doesNotLeakThePlaintext(t *testing.T) {
	c, err := crypto.NewCipher(testKey)
	require.NoError(t, err)

	fp := c.Fingerprint("a-very-distinctive-refresh-token")

	assert.NotContains(t, fp, "distinctive")
	assert.NotEmpty(t, fp)
	assert.Len(t, fp, 64, "hex-encoded SHA-256")
}

// An empty token has no fingerprint. Returning a valid-looking one would let a
// row with no refresh token participate in a compare-and-swap.
func TestFingerprint_ofEmptyIsEmpty(t *testing.T) {
	c, err := crypto.NewCipher(testKey)
	require.NoError(t, err)

	assert.Empty(t, c.Fingerprint(""))
	assert.Empty(t, c.Fingerprint("   "))
}
