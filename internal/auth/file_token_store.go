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
)

// FileTokenStore encrypts OAuth2 tokens with AES-256-GCM and persists them to disk.
// The encryption key must be 32 bytes (AES-256).
type FileTokenStore struct {
	path string
	gcm  cipher.AEAD
}

// NewFileTokenStore creates a FileTokenStore at path using the given 32-byte AES key.
// The parent directory is created if it does not exist.
// The key must come from the system keyring; it is the caller's responsibility
// never to log or hardcode it.
func NewFileTokenStore(path string, key []byte) (*FileTokenStore, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("auth: encryption key must be 32 bytes, got %d", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("auth: create cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("auth: create GCM: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("auth: create token dir: %w", err)
	}
	return &FileTokenStore{path: path, gcm: gcm}, nil
}

// Save encrypts token and writes it to disk, overwriting any previous token.
func (s *FileTokenStore) Save(token *Token) error {
	plain, err := json.Marshal(token)
	if err != nil {
		return fmt.Errorf("auth: marshal token: %w", err)
	}
	nonce := make([]byte, s.gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return fmt.Errorf("auth: generate nonce: %w", err)
	}
	ciphertext := s.gcm.Seal(nonce, nonce, plain, nil)
	if err := os.WriteFile(s.path, ciphertext, 0o600); err != nil {
		return fmt.Errorf("auth: write token file: %w", err)
	}
	return nil
}

// Load decrypts and returns the stored token.
// Returns ErrNoToken if no token has been saved yet.
func (s *FileTokenStore) Load() (*Token, error) {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNoToken
	}
	if err != nil {
		return nil, fmt.Errorf("auth: read token file: %w", err)
	}
	if len(data) < s.gcm.NonceSize() {
		return nil, fmt.Errorf("auth: token file too short to be valid ciphertext")
	}
	nonce, ciphertext := data[:s.gcm.NonceSize()], data[s.gcm.NonceSize():]
	plain, err := s.gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("auth: decrypt token: %w", err)
	}
	var token Token
	if err := json.Unmarshal(plain, &token); err != nil {
		return nil, fmt.Errorf("auth: unmarshal token: %w", err)
	}
	return &token, nil
}
