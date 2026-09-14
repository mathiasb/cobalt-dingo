package fortnox

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/mathiasb/cobalt-dingo/internal/config"
	"github.com/mathiasb/cobalt-dingo/internal/domain"
	"github.com/mathiasb/cobalt-dingo/internal/fortnox"
)

// RefreshFunc exchanges a refresh token for a new token pair. Injected so the
// store is testable without the Fortnox token endpoint.
type RefreshFunc func(refreshToken string) (domain.OAuthToken, error)

// RefreshingTokenStore is a domain.TokenStore that refreshes on Load.
//
// It is a decorator rather than a method on each adapter because every ledger
// adapter takes a TokenStore and only calls Load — so before this, all of them
// 401'd on an expired token and only Connector, which has the logic inline,
// did not. One place to be right.
//
// minLifetime is the headroom it insists on, and it is the reason this is not
// simply a call to OAuthToken.Valid(). Valid() allows a 30-second margin,
// which is ample for one request and useless for reading a 405-voucher year:
// the work takes about two minutes, so a token that passes Valid() can die
// mid-fetch after several hundred requests. The store refreshes anything that
// cannot outlast the work it is about to be used for.
type RefreshingTokenStore struct {
	inner       domain.TokenStore
	refresh     RefreshFunc
	minLifetime time.Duration
	now         func() time.Time
	log         *slog.Logger
}

var _ domain.TokenStore = (*RefreshingTokenStore)(nil)

// NewRefreshingTokenStore wraps inner. now is injected for testability.
func NewRefreshingTokenStore(
	inner domain.TokenStore,
	refresh RefreshFunc,
	minLifetime time.Duration,
	now func() time.Time,
	log *slog.Logger,
) *RefreshingTokenStore {
	if now == nil {
		now = time.Now
	}
	return &RefreshingTokenStore{inner: inner, refresh: refresh, minLifetime: minLifetime, now: now, log: log}
}

// NewFortnoxRefreshingTokenStore wraps inner with refreshes against the real
// Fortnox token endpoint, using the mode's client credentials.
func NewFortnoxRefreshingTokenStore(
	inner domain.TokenStore,
	cfg config.Fortnox,
	minLifetime time.Duration,
	log *slog.Logger,
) *RefreshingTokenStore {
	return NewRefreshingTokenStore(inner, func(refreshToken string) (domain.OAuthToken, error) {
		t, err := fortnox.RefreshAccessToken(cfg.ClientID, cfg.ClientSecret, refreshToken)
		if err != nil {
			return domain.OAuthToken{}, err
		}
		return domain.OAuthToken{AccessToken: t.AccessToken, RefreshToken: t.RefreshToken, ExpiresAt: t.ExpiresAt}, nil
	}, minLifetime, time.Now, log)
}

// Load returns a token with at least minLifetime remaining, refreshing first
// if necessary.
func (s *RefreshingTokenStore) Load(ctx context.Context, tenantID domain.TenantID) (domain.OAuthToken, error) {
	tok, err := s.inner.Load(ctx, tenantID)
	if err != nil {
		return domain.OAuthToken{}, fmt.Errorf("load token: %w", err)
	}
	if tok.AccessToken != "" && s.now().Add(s.minLifetime).Before(tok.ExpiresAt) {
		return tok, nil
	}
	if tok.RefreshToken == "" {
		return domain.OAuthToken{}, fmt.Errorf(
			"token for %s expires at %s and has no refresh token — reconnect the company",
			tenantID, tok.ExpiresAt.Format(time.RFC3339))
	}

	newTok, err := s.refresh(tok.RefreshToken)
	if err != nil {
		// No fallback to the token we hold. Serving it produces a 401 several
		// hundred requests later, attributed to whatever was running then
		// rather than to the refresh that actually failed.
		return domain.OAuthToken{}, fmt.Errorf("refresh token for %s: %w", tenantID, err)
	}

	if err := s.inner.AtomicRefresh(ctx, tenantID, tok, newTok); err != nil {
		if !errors.Is(err, domain.ErrTokenConflict) {
			return domain.OAuthToken{}, fmt.Errorf("persist refreshed token for %s: %w", tenantID, err)
		}
		// Another process refreshed first. Fortnox rotates the refresh token
		// on every use, so the one we just minted is already dead — the
		// winner's token is the only usable one.
		s.log.Info("token refresh conflict: using the winner's token", "tenant", tenantID)
		tok, err = s.inner.Load(ctx, tenantID)
		if err != nil {
			return domain.OAuthToken{}, fmt.Errorf("reload token for %s after conflict: %w", tenantID, err)
		}
		return tok, nil
	}
	return newTok, nil
}

func (s *RefreshingTokenStore) Save(ctx context.Context, tenantID domain.TenantID, tok domain.OAuthToken) error {
	return s.inner.Save(ctx, tenantID, tok)
}

func (s *RefreshingTokenStore) AtomicRefresh(ctx context.Context, tenantID domain.TenantID, old, newToken domain.OAuthToken) error {
	return s.inner.AtomicRefresh(ctx, tenantID, old, newToken)
}

func (s *RefreshingTokenStore) Delete(ctx context.Context, tenantID domain.TenantID) error {
	return s.inner.Delete(ctx, tenantID)
}
