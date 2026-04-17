package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/mathiasb/cobalt-dingo/internal/adapter/postgres/pgstore"
	"github.com/mathiasb/cobalt-dingo/internal/domain"
)

// TokenStore implements domain.TokenStore using PostgreSQL.
// AtomicRefresh uses a conditional UPDATE to guarantee CAS semantics across
// multiple server replicas — safe where file.TokenStore is not.
type TokenStore struct{ s *Store }

// NewTokenStore returns a TokenStore backed by s.
func NewTokenStore(s *Store) *TokenStore { return &TokenStore{s: s} }

// Load implements domain.TokenStore.
func (t *TokenStore) Load(ctx context.Context, tenantID domain.TenantID) (domain.OAuthToken, error) {
	row, err := t.s.queries.GetToken(ctx, string(tenantID))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.OAuthToken{}, fmt.Errorf("no token for tenant %s", tenantID)
	}
	if err != nil {
		return domain.OAuthToken{}, fmt.Errorf("load token: %w", err)
	}
	return domain.OAuthToken{
		AccessToken:  row.AccessToken,
		RefreshToken: row.RefreshToken,
		ExpiresAt:    row.ExpiresAt,
	}, nil
}

// AtomicRefresh implements domain.TokenStore.
// Issues an UPDATE WHERE refresh_token = old.RefreshToken; returns
// domain.ErrTokenConflict if zero rows are updated (another replica won the race).
func (t *TokenStore) AtomicRefresh(ctx context.Context, tenantID domain.TenantID, old, newToken domain.OAuthToken) error {
	_, err := t.s.queries.AtomicRefreshToken(ctx, pgstore.AtomicRefreshTokenParams{
		TenantID:       string(tenantID),
		RefreshToken:   old.RefreshToken,
		AccessToken:    newToken.AccessToken,
		RefreshToken_2: newToken.RefreshToken,
		ExpiresAt:      newToken.ExpiresAt,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ErrTokenConflict
	}
	if err != nil {
		return fmt.Errorf("atomic refresh token: %w", err)
	}
	return nil
}
