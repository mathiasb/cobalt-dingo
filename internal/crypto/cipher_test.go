package crypto_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mathiasb/cobalt-dingo/internal/crypto"
)

// A 32-byte key, base64. Test-only; generated, not a real secret.
const (
	testKey  = "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8="
	otherKey = "f39/f39/f39/f39/f39/f39/f39/f39/f39/f39/f38="
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
		"empty":          "",
		"not base64":     "%%%not-base64%%%",
		"too short":      "c2hvcnQ=",
		"one byte short": "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHg==",

		// These two are the only cases the explicit length check actually
		// catches, and they are the reason it is not redundant: aes.NewCipher
		// accepts 16- and 24-byte keys as AES-128 and AES-192. Without the
		// check, configuring a short-but-valid key silently downgrades the
		// cipher instead of failing — found by mutating the check out and
		// watching every other case still pass.
		"valid AES-128 key": "AAECAwQFBgcICQoLDA0ODw==",
		"valid AES-192 key": "AAECAwQFBgcICQoLDA0ODxAREhMUFRYX",
	}
	for name, key := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := crypto.NewCipher(key)
			require.Error(t, err)
		})
	}
}

// The error must never quote the key or the plaintext — these reach logs.
func TestNewCipher_errorDoesNotEchoTheKey(t *testing.T) {
	_, err := crypto.NewCipher("c2hvcnQ=")

	require.Error(t, err)
	assert.NotContains(t, err.Error(), "c2hvcnQ")
}
