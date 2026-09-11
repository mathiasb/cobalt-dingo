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
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
)

// keyLen is 32 bytes — AES-256. Fixed rather than inferred from the key's
// length, so a short key is an error instead of a silent downgrade to AES-128.
const keyLen = 32

// Cipher seals and opens secrets with one master key.
type Cipher struct{ aead cipher.AEAD }

// NewCipher builds a Cipher from a base64-encoded 32-byte key.
//
// It refuses an unusable key here, at configuration time, rather than letting
// the failure surface when a tenant first tries to connect. Errors never quote
// the key: they reach logs.
func NewCipher(base64Key string) (*Cipher, error) {
	if base64Key == "" {
		return nil, errors.New("no encryption key configured: refusing to store tenant secrets unencrypted")
	}
	key, err := base64.StdEncoding.DecodeString(base64Key)
	if err != nil {
		return nil, errors.New("encryption key is not valid base64")
	}
	if len(key) != keyLen {
		return nil, fmt.Errorf("encryption key must be %d bytes (got %d): generate one with `openssl rand -base64 32`", keyLen, len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("build AES cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("build GCM: %w", err)
	}
	return &Cipher{aead: aead}, nil
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
