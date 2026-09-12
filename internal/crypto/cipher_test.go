package crypto_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mathiasb/cobalt-dingo/internal/crypto"
)

// Test-only high-entropy secrets, shaped like 1Password's
// `letters,digits,symbols,64` recipe. Not real secrets.
const (
	testKey  = "Kq7#vP2mZx9!tR4wLs8@nB6yHj3^dF5gCe1%uA0oXi7*qW2zTv4rMk9$bN6pJ8hY"
	otherKey = "Zb3!mK8wQr2@yT5nHd7^pL4gXs9%vC6jFe1oUa0#iR7*zN2tBk9$qM6wJ8hYpD4"
)

func TestSeal_roundTripsThroughOpen(t *testing.T) {
	c, err := crypto.NewCipher(testKey)
	require.NoError(t, err)

	sealed, err := c.Seal("fortnox-client-secret-value")
	require.NoError(t, err)

	got, err := c.Open(sealed)
	require.NoError(t, err)
	assert.Equal(t, "fortnox-client-secret-value", got)
}

// The whole point. If the ciphertext contained the plaintext, encrypting at
// rest would be theatre.
func TestSeal_doesNotContainThePlaintext(t *testing.T) {
	c, err := crypto.NewCipher(testKey)
	require.NoError(t, err)

	sealed, err := c.Seal("a-very-distinctive-secret")
	require.NoError(t, err)

	assert.NotContains(t, sealed, "a-very-distinctive-secret")
	assert.NotContains(t, strings.ToLower(sealed), "distinctive")
}

// Same plaintext twice must not produce the same ciphertext, or the database
// leaks which tenants share a value — and a nonce reuse in GCM is fatal.
func TestSeal_usesAFreshNonceEachTime(t *testing.T) {
	c, err := crypto.NewCipher(testKey)
	require.NoError(t, err)

	first, err := c.Seal("same-input")
	require.NoError(t, err)
	second, err := c.Seal("same-input")
	require.NoError(t, err)

	assert.NotEqual(t, first, second)
}

// A wrong key must fail loudly. Returning garbage would put a corrupt client
// secret into an OAuth request, and the error would surface at Fortnox as an
// opaque rejection.
func TestOpen_failsWithTheWrongKey(t *testing.T) {
	sealer, err := crypto.NewCipher(testKey)
	require.NoError(t, err)
	sealed, err := sealer.Seal("secret")
	require.NoError(t, err)

	other, err := crypto.NewCipher(otherKey)
	require.NoError(t, err)

	_, err = other.Open(sealed)
	require.Error(t, err)
}

// GCM authenticates as well as encrypts, so a tampered row is detected rather
// than silently decrypted into something else.
func TestOpen_rejectsTamperedCiphertext(t *testing.T) {
	c, err := crypto.NewCipher(testKey)
	require.NoError(t, err)
	sealed, err := c.Seal("secret")
	require.NoError(t, err)

	tampered := []byte(sealed)
	tampered[len(tampered)-2] ^= 0xFF // flip bits inside the base64 payload

	_, err = c.Open(string(tampered))
	require.Error(t, err)
}

func TestOpen_rejectsGarbage(t *testing.T) {
	c, err := crypto.NewCipher(testKey)
	require.NoError(t, err)

	for _, in := range []string{"", "not-base64!!", "aGVsbG8="} {
		_, err := c.Open(in)
		assert.Errorf(t, err, "input %q must not decrypt", in)
	}
}

// Fail closed at construction. A short or absent key must be refused where it
// is configured, not discovered later when a tenant tries to connect.
func TestNewCipher_refusesAnUnusableKey(t *testing.T) {
	cases := map[string]string{
		"empty": "",
		// The key is DERIVED from this secret with HKDF, so its format is free
		// — but its entropy is not. A short secret is refused at configuration
		// time, because HKDF cannot manufacture entropy the input lacks, and a
		// fast KDF over a guessable input is brute-forceable offline against a
		// stolen database.
		"one character":    "x",
		"short passphrase": "hunter2",
		"31 characters":    "0123456789012345678901234567890",
	}
	for name, key := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := crypto.NewCipher(key)
			require.Error(t, err)
		})
	}
}

// 32 characters is the documented floor and must be accepted, or the boundary
// is untested in the direction provisioning actually depends on.
func TestNewCipher_acceptsTheMinimumLength(t *testing.T) {
	_, err := crypto.NewCipher("01234567890123456789012345678901")

	require.NoError(t, err)
}

// The derivation must actually depend on its input.
func TestNewCipher_derivesADistinctKeyPerSecret(t *testing.T) {
	a, err := crypto.NewCipher(testKey)
	require.NoError(t, err)
	b, err := crypto.NewCipher(otherKey)
	require.NoError(t, err)

	sealedByA, err := a.Seal("secret")
	require.NoError(t, err)

	_, err = b.Open(sealedByA)
	require.Error(t, err, "a different configured secret must not open A's ciphertext")
}

// The error must never quote the key or the plaintext — these reach logs.
func TestNewCipher_errorDoesNotEchoTheKey(t *testing.T) {
	_, err := crypto.NewCipher("hunter2")

	require.Error(t, err)
	assert.NotContains(t, err.Error(), "hunter2")
}
