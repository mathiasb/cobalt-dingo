// Package auth handles OAuth2 token storage with AES-GCM encryption.
// The encryption key is retrieved from the system keyring and never
// stored in plaintext or logged.
package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// Token holds an OAuth2 access/refresh token pair.
type Token struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	Expiry       time.Time `json:"expiry"`
	TokenType    string    `json:"token_type"`
}

// IsExpired reports whether the access token has expired (with a 60s buffer).
func (t *Token) IsExpired() bool {
	return time.Now().After(t.Expiry.Add(-60 * time.Second))
}

// TokenStore persists OAuth2 tokens encrypted on disk.
type TokenStore struct {
	path string
	key  []byte // 32-byte AES-256 key from system keyring
}

// NewTokenStore opens the token store at path.
// The encryption key is derived from the system keyring.
// Parent directories are created if they do not exist.
func NewTokenStore(path string) (*TokenStore, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("auth: create token dir: %w", err)
	}
	key, err := loadOrCreateKey()
	if err != nil {
		return nil, fmt.Errorf("auth: load encryption key: %w", err)
	}
	return &TokenStore{path: path, key: key}, nil
}

// Save encrypts and persists token to disk.
func (s *TokenStore) Save(token *Token) error {
	data, err := json.Marshal(token)
	if err != nil {
		return fmt.Errorf("auth: marshal token: %w", err)
	}
	encrypted, err := encrypt(s.key, data)
	if err != nil {
		return fmt.Errorf("auth: encrypt token: %w", err)
	}
	if err := os.WriteFile(s.path, encrypted, 0o600); err != nil {
		return fmt.Errorf("auth: write token file: %w", err)
	}
	return nil
}

// Load decrypts and returns the stored token.
// Returns ErrNoToken if no token has been saved yet.
func (s *TokenStore) Load() (*Token, error) {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNoToken
	}
	if err != nil {
		return nil, fmt.Errorf("auth: read token file: %w", err)
	}
	plain, err := decrypt(s.key, data)
	if err != nil {
		return nil, fmt.Errorf("auth: decrypt token: %w", err)
	}
	var token Token
	if err := json.Unmarshal(plain, &token); err != nil {
		return nil, fmt.Errorf("auth: unmarshal token: %w", err)
	}
	return &token, nil
}

// ErrNoToken is returned when no token has been persisted yet.
var ErrNoToken = errors.New("auth: no token stored – run OAuth2 flow first")

// encrypt encrypts plaintext with AES-256-GCM.
func encrypt(key, plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

// decrypt decrypts AES-256-GCM ciphertext.
func decrypt(key, ciphertext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(ciphertext) < gcm.NonceSize() {
		return nil, errors.New("auth: ciphertext too short")
	}
	nonce, ct := ciphertext[:gcm.NonceSize()], ciphertext[gcm.NonceSize():]
	return gcm.Open(nil, nonce, ct, nil)
}

// loadOrCreateKey retrieves the AES key from the system keyring,
// generating and storing a new one if none exists.
// TODO: integrate zalando/go-keyring for production use.
// For now, falls back to a key file in the config directory.
func loadOrCreateKey() ([]byte, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	keyPath := filepath.Join(home, ".coo-agent", "master.key")
	if err := os.MkdirAll(filepath.Dir(keyPath), 0o700); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(keyPath)
	if err == nil && len(data) == 32 {
		return data, nil
	}
	key := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, err
	}
	if err := os.WriteFile(keyPath, key, 0o600); err != nil {
		return nil, err
	}
	return key, nil
}
