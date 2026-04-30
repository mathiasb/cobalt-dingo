// Package file provides a file-based implementation of domain.TokenStore.
// Suitable for single-process development; replace with adapter/postgres for production.
package file

import (
	"context"
	"fmt"
	"sync"

	"github.com/mathiasb/cobalt-dingo/internal/domain"
	"github.com/mathiasb/cobalt-dingo/internal/fortnox"
)

// TokenStore implements domain.TokenStore using a mode-specific token file
// on disk (e.g. .fortnox-tokens-sandbox.json or .fortnox-tokens-real-ro.json).
// A mutex provides in-process CAS semantics; it does not protect against
// concurrent processes — use the postgres adapter in production.
type TokenStore struct {
	mu   sync.Mutex
	path string
}

// NewTokenStore returns a file-backed TokenStore reading and writing the
// given path. Pass config.Mode.TokenFile() to keep modes isolated.
func NewTokenStore(path string) *TokenStore {
	return &TokenStore{path: path}
}

// Load reads the current token from disk. tenantID is ignored (single-tenant).
func (s *TokenStore) Load(_ context.Context, _ domain.TenantID) (domain.OAuthToken, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	t, err := fortnox.LoadToken(s.path)
	if err != nil {
		return domain.OAuthToken{}, fmt.Errorf("file token store load: %w", err)
	}
	return domain.OAuthToken{
		AccessToken:  t.AccessToken,
		RefreshToken: t.RefreshToken,
		ExpiresAt:    t.ExpiresAt,
	}, nil
}

// AtomicRefresh writes newToken only when the stored refresh token matches old.RefreshToken.
// Returns domain.ErrTokenConflict if the stored token has already been replaced.
func (s *TokenStore) AtomicRefresh(_ context.Context, _ domain.TenantID, old, newToken domain.OAuthToken) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	current, err := fortnox.LoadToken(s.path)
	if err != nil {
		return fmt.Errorf("file token store read: %w", err)
	}
	if current.RefreshToken != old.RefreshToken {
		return domain.ErrTokenConflict
	}

	t := fortnox.Token{
		AccessToken:  newToken.AccessToken,
		RefreshToken: newToken.RefreshToken,
		ExpiresAt:    newToken.ExpiresAt,
	}
	if err := fortnox.SaveToken(s.path, t); err != nil {
		return fmt.Errorf("file token store write: %w", err)
	}
	return nil
}
