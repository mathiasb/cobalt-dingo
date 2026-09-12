// Package crypto encrypts tenant-supplied secrets for storage at rest.
//
// It exists because cobalt-dingo is moving to per-tenant Fortnox integrations
// (#46): each tenant registers their own connected app and supplies its client
// secret. A client secret in a database column in plaintext would be a worse
// posture than the ESO-injected environment variable it replaces, so the row is
// encrypted and only the master key lives in a vault.
//
// Scope, stated so it is not mistaken for more: this is a single-key AES-GCM
// wrapper, not a key-management system. It is the right size for one operator
// with one master key in 1Password AgentSecrets. Envelope encryption with
// per-row data keys buys key rotation without re-encrypting, and is worth
// revisiting if the tenant count or the compliance surface grows.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

// keyLen is 32 bytes — AES-256.
const keyLen = 32

// minSecretLen is the floor on the CONFIGURED secret, not on the derived key.
//
// HKDF cannot manufacture entropy the input does not have, and it is a fast
// KDF by design — so a guessable secret is brute-forceable offline against a
// stolen database. 32 characters is comfortably above what 1Password's
// `letters,digits,symbols,64` recipe produces, which is how these are
// provisioned (infra scripts/new-agent-secret.sh).
const minSecretLen = 32

// hkdfInfo domain-separates this key from any other use of the same secret, so
// a future second purpose cannot accidentally derive the same bytes.
const hkdfInfo = "cobalt-dingo/fortnox-integration-client-secret/v1"

// Cipher seals and opens secrets, and fingerprints them for comparison.
type Cipher struct {
	aead cipher.AEAD

	// fpKey is derived from the same secret under a different info string, so
	// encryption and fingerprinting do not share a key.
	fpKey []byte
}

// NewCipher derives an AES-256 key from the configured secret via HKDF-SHA256.
//
// The secret's FORMAT is deliberately free — any sufficiently long string works
// — so the estate's existing secret-provisioning path can be used unchanged
// rather than requiring a base64-encoded 32-byte blob. Its LENGTH is not free:
// see minSecretLen.
//
// A salt is deliberately absent. HKDF's salt is optional and its value here
// would have to be stored alongside the ciphertext or fixed in code; with a
// high-entropy input the extract step needs no salt to be sound, and a fixed
// salt in code provides nothing a fixed info string does not.
//
// It refuses an unusable secret here, at configuration time, rather than
// letting the failure surface when a tenant first connects. Errors never quote
// the secret: they reach logs.
func NewCipher(secret string) (*Cipher, error) {
	if secret == "" {
		return nil, errors.New("no encryption secret configured: refusing to store tenant secrets unencrypted")
	}
	if len(secret) < minSecretLen {
		return nil, fmt.Errorf("encryption secret must be at least %d characters (got %d): HKDF cannot add entropy the input lacks", minSecretLen, len(secret))
	}

	key, err := hkdf.Key(sha256.New, []byte(secret), nil, hkdfInfo, keyLen)
	if err != nil {
		return nil, fmt.Errorf("derive key: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("build AES cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("build GCM: %w", err)
	}
	fpKey, err := hkdf.Key(sha256.New, []byte(secret), nil, fingerprintInfo, keyLen)
	if err != nil {
		return nil, fmt.Errorf("derive fingerprint key: %w", err)
	}

	return &Cipher{aead: aead, fpKey: fpKey}, nil
}

// Seal encrypts plaintext and returns base64(nonce || ciphertext).
//
// A fresh random nonce per call is not optional: GCM loses all confidentiality
// guarantees on nonce reuse, and a deterministic ciphertext would also reveal
// which tenants share a value.
func (c *Cipher) Seal(plaintext string) (string, error) {
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("generate nonce: %w", err)
	}
	sealed := c.aead.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(sealed), nil
}

// Open reverses Seal.
//
// GCM authenticates, so a tampered or wrongly-keyed row fails here rather than
// decrypting into something else — which would put a corrupt client secret into
// an OAuth request and surface at Fortnox as an opaque rejection.
func (c *Cipher) Open(sealed string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(sealed)
	if err != nil {
		return "", errors.New("stored secret is not valid base64")
	}
	ns := c.aead.NonceSize()
	if len(raw) < ns {
		return "", errors.New("stored secret is too short to contain a nonce")
	}
	plaintext, err := c.aead.Open(nil, raw[:ns], raw[ns:], nil)
	if err != nil {
		// Deliberately not wrapped: the underlying message is "message
		// authentication failed", which says nothing useful and invites
		// guessing. The cause is a wrong key or a tampered row.
		return "", errors.New("stored secret failed authentication: wrong key, or the row was modified")
	}
	return string(plaintext), nil
}

// fingerprintInfo derives a SEPARATE key for fingerprinting.
//
// Domain separation is not decorative here: using the encryption key as an HMAC
// key means one secret serving two cryptographic purposes, and a weakness in
// either use would reach the other.
const fingerprintInfo = "cobalt-dingo/refresh-token-fingerprint/v1"

// Fingerprint returns a deterministic, keyed fingerprint of plaintext.
//
// It exists because encrypted columns cannot be compared. The rolling-refresh
// race is detected by `UPDATE ... WHERE refresh_token_fp = <old>`, and GCM
// ciphertext is randomised — the same token seals differently every time, so
// matching on the ciphertext would never succeed and the race detection would
// silently stop working. That is worse than the plaintext it replaces: two
// processes would both believe they won.
//
// HMAC rather than a bare hash: a database dump must not let someone confirm a
// guessed token by hashing it.
//
// An empty input has no fingerprint. Returning a real one would let a row with
// no refresh token take part in a compare-and-swap.
func (c *Cipher) Fingerprint(plaintext string) string {
	if strings.TrimSpace(plaintext) == "" {
		return ""
	}
	mac := hmac.New(sha256.New, c.fpKey)
	mac.Write([]byte(plaintext))
	return hex.EncodeToString(mac.Sum(nil))
}
