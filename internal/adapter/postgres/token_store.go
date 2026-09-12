package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/mathiasb/cobalt-dingo/internal/adapter/postgres/pgstore"
	"github.com/mathiasb/cobalt-dingo/internal/crypto"
	"github.com/mathiasb/cobalt-dingo/internal/domain"
)

// TokenStore implements domain.TokenStore using PostgreSQL, with the tokens
// encrypted at rest (#85).
//
// AtomicRefresh uses a conditional UPDATE to guarantee CAS semantics across
// multiple server replicas — safe where file.TokenStore is not. It matches on a
// deterministic fingerprint rather than the ciphertext, because GCM is
// randomised: comparing sealed values would never match, and the race
// detection would stop working silently. Two processes would then both believe
// they had won, which is worse than the plaintext this replaces.
type TokenStore struct {
	s      *Store
	cipher *crypto.Cipher
}

// NewTokenStore returns a TokenStore backed by s.
//
// cipher is required. A nil one would mean writing plaintext into columns whose
// whole purpose is to not hold it, so every method refuses instead.
func NewTokenStore(s *Store, cipher *crypto.Cipher) *TokenStore {
	return &TokenStore{s: s, cipher: cipher}
}

// errNoCipher is returned rather than falling back to plaintext. Storing an
// unencrypted token in a column named _sealed would be undetectable by
// inspection and would defeat the point of the change.
var errNoCipher = errors.New("token store has no encryption key: refusing to read or write tokens unencrypted")

// Load implements domain.TokenStore.
func (t *TokenStore) Load(ctx context.Context, tenantID domain.TenantID) (domain.OAuthToken, error) {
	if t.cipher == nil {
		return domain.OAuthToken{}, errNoCipher
	}
	row, err := t.s.queries.GetToken(ctx, string(tenantID))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.OAuthToken{}, fmt.Errorf("no token for tenant %s", tenantID)
	}
	if err != nil {
		return domain.OAuthToken{}, fmt.Errorf("load token: %w", err)
	}

	access, err := t.cipher.Open(row.AccessTokenSealed)
	if err != nil {
		// Deliberately does not name the tenant's token or the key. A wrong
		// key or a tampered row lands here.
		return domain.OAuthToken{}, fmt.Errorf("decrypt access token for tenant %s: %w", tenantID, err)
	}
	refresh, err := t.cipher.Open(row.RefreshTokenSealed)
	if err != nil {
		return domain.OAuthToken{}, fmt.Errorf("decrypt refresh token for tenant %s: %w", tenantID, err)
	}

	return domain.OAuthToken{
		AccessToken:  access,
		RefreshToken: refresh,
		ExpiresAt:    row.ExpiresAt,
	}, nil
}

// Save implements domain.TokenStore — unconditionally upserts the token.
func (t *TokenStore) Save(ctx context.Context, tenantID domain.TenantID, token domain.OAuthToken) error {
	if t.cipher == nil {
		return errNoCipher
	}
	access, refresh, err := t.seal(token)
	if err != nil {
		return err
	}
	if err := t.s.queries.UpsertToken(ctx, pgstore.UpsertTokenParams{
		TenantID:           string(tenantID),
		AccessTokenSealed:  access,
		RefreshTokenSealed: refresh,
		RefreshTokenFp:     t.cipher.Fingerprint(token.RefreshToken),
		ExpiresAt:          token.ExpiresAt,
	}); err != nil {
		return fmt.Errorf("save token: %w", err)
	}
	return nil
}

// AtomicRefresh implements domain.TokenStore.
//
// Returns domain.ErrTokenConflict if zero rows are updated, which means another
// replica refreshed first — Fortnox rotates the refresh token on use and
// invalidates the old one, so the loser must re-read rather than retry.
func (t *TokenStore) AtomicRefresh(ctx context.Context, tenantID domain.TenantID, old, newToken domain.OAuthToken) error {
	if t.cipher == nil {
		return errNoCipher
	}
	oldFingerprint := t.cipher.Fingerprint(old.RefreshToken)
	if oldFingerprint == "" {
		// An empty fingerprint would match no row, which would be reported as
		// a conflict and send the caller into a re-read loop over a condition
		// that can never become true.
		return errors.New("atomic refresh needs the refresh token that was read")
	}

	access, refresh, err := t.seal(newToken)
	if err != nil {
		return err
	}
	_, err = t.s.queries.AtomicRefreshToken(ctx, pgstore.AtomicRefreshTokenParams{
		TenantID:           string(tenantID),
		RefreshTokenFp:     oldFingerprint,
		AccessTokenSealed:  access,
		RefreshTokenSealed: refresh,
		RefreshTokenFp_2:   t.cipher.Fingerprint(newToken.RefreshToken),
		ExpiresAt:          newToken.ExpiresAt,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ErrTokenConflict
	}
	if err != nil {
		return fmt.Errorf("atomic refresh token: %w", err)
	}
	return nil
}

// seal encrypts both halves of a token, refusing an incomplete one.
func (t *TokenStore) seal(token domain.OAuthToken) (access, refresh string, err error) {
	if token.AccessToken == "" || token.RefreshToken == "" {
		return "", "", errors.New("refusing to store an incomplete token")
	}
	if access, err = t.cipher.Seal(token.AccessToken); err != nil {
		return "", "", fmt.Errorf("seal access token: %w", err)
	}
	if refresh, err = t.cipher.Seal(token.RefreshToken); err != nil {
		return "", "", fmt.Errorf("seal refresh token: %w", err)
	}
	return access, refresh, nil
}

// Delete implements domain.TokenStore — removes the token for tenantID.
func (t *TokenStore) Delete(ctx context.Context, tenantID domain.TenantID) error {
	if err := t.s.queries.DeleteToken(ctx, string(tenantID)); err != nil {
		return fmt.Errorf("delete token: %w", err)
	}
	return nil
}
