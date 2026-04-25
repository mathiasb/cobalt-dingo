package fortnox_test

import (
	"context"
	"time"

	"github.com/mathiasb/cobalt-dingo/internal/domain"
)

// stubTokenStore is a minimal domain.TokenStore that always returns a fixed, valid token.
// Shared across all adapter tests in this package.
type stubTokenStore struct{}

func newStubTokenStore() *stubTokenStore { return &stubTokenStore{} }

func (s *stubTokenStore) Load(_ context.Context, _ domain.TenantID) (domain.OAuthToken, error) {
	return domain.OAuthToken{
		AccessToken:  "test-access-token",
		RefreshToken: "test-refresh",
		ExpiresAt:    time.Now().Add(24 * time.Hour),
	}, nil
}

func (s *stubTokenStore) AtomicRefresh(_ context.Context, _ domain.TenantID, _, _ domain.OAuthToken) error {
	return nil
}
