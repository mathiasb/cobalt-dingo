package fortnox_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	adapterfortnox "github.com/mathiasb/cobalt-dingo/internal/adapter/fortnox"
	"github.com/mathiasb/cobalt-dingo/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type recordingStore struct {
	stored        domain.OAuthToken
	afterConflict domain.OAuthToken
	conflict      bool
	refreshCalls  int
	loads         int
}

func (s *recordingStore) Load(context.Context, domain.TenantID) (domain.OAuthToken, error) {
	s.loads++
	if s.conflict && s.loads > 1 {
		return s.afterConflict, nil
	}
	return s.stored, nil
}

func (s *recordingStore) Save(_ context.Context, _ domain.TenantID, t domain.OAuthToken) error {
	s.stored = t
	return nil
}

func (s *recordingStore) AtomicRefresh(_ context.Context, _ domain.TenantID, _, newToken domain.OAuthToken) error {
	s.refreshCalls++
	if s.conflict {
		return domain.ErrTokenConflict
	}
	s.stored = newToken
	return nil
}

func (s *recordingStore) Delete(context.Context, domain.TenantID) error { return nil }

func quietLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// Anchored to the real clock, not a fixed date. domain.OAuthToken.Valid()
// reads time.Now() directly, so a fixture dated in the past makes Valid()
// false for free — and a test that refreshes because the token is long expired
// proves nothing about the headroom rule it is meant to pin. Found by
// mutation: swapping the headroom check for Valid() left this suite green.
var testNow = time.Now()

func at(minutes int) time.Time {
	return testNow.Add(time.Duration(minutes) * time.Minute)
}

func minted() domain.OAuthToken {
	return domain.OAuthToken{AccessToken: "fresh", RefreshToken: "r2", ExpiresAt: at(60)}
}

var _ domain.TokenStore = (*adapterfortnox.RefreshingTokenStore)(nil)

func TestRefreshingTokenStore_leavesAHealthyTokenAlone(t *testing.T) {
	inner := &recordingStore{stored: domain.OAuthToken{AccessToken: "live", RefreshToken: "r1", ExpiresAt: at(50)}}
	var calls int
	s := adapterfortnox.NewRefreshingTokenStore(inner, func(string) (domain.OAuthToken, error) {
		calls++
		return minted(), nil
	}, 10*time.Minute, func() time.Time { return at(0) }, quietLog())

	tok, err := s.Load(context.Background(), "t1")
	require.NoError(t, err)
	assert.Equal(t, "live", tok.AccessToken)
	assert.Zero(t, calls)
}

// The case that made this exist. domain.OAuthToken.Valid() allows a 30-second
// margin, so a token expiring in 40 seconds is "valid" — and reading a
// 405-voucher year takes about two minutes. The fetch would start on a good
// token and 401 halfway through, after several hundred requests.
func TestRefreshingTokenStore_refreshesAValidTokenThatCannotOutlastTheWork(t *testing.T) {
	inner := &recordingStore{stored: domain.OAuthToken{AccessToken: "expiring", RefreshToken: "r1", ExpiresAt: at(2)}}
	var calls int
	s := adapterfortnox.NewRefreshingTokenStore(inner, func(rt string) (domain.OAuthToken, error) {
		calls++
		assert.Equal(t, "r1", rt)
		return minted(), nil
	}, 10*time.Minute, func() time.Time { return at(0) }, quietLog())

	tok, err := s.Load(context.Background(), "t1")
	require.NoError(t, err)
	assert.Equal(t, "fresh", tok.AccessToken)
	assert.Equal(t, 1, calls)
	assert.Equal(t, 1, inner.refreshCalls, "the new token must be persisted")
}

// Fortnox rotates the refresh token on every use, so the loser of a concurrent
// refresh holds one that is already dead. It must use the winner's token, not
// the one it minted.
func TestRefreshingTokenStore_onConflictUsesTheWinnersToken(t *testing.T) {
	inner := &recordingStore{
		stored:        domain.OAuthToken{AccessToken: "expired", RefreshToken: "r1", ExpiresAt: at(-1)},
		afterConflict: domain.OAuthToken{AccessToken: "winner", RefreshToken: "r9", ExpiresAt: at(60)},
		conflict:      true,
	}
	s := adapterfortnox.NewRefreshingTokenStore(inner, func(string) (domain.OAuthToken, error) {
		return minted(), nil
	}, 10*time.Minute, func() time.Time { return at(0) }, quietLog())

	tok, err := s.Load(context.Background(), "t1")
	require.NoError(t, err)
	assert.Equal(t, "winner", tok.AccessToken)
}

// A failed refresh must not hand back the expired token. It would produce a
// 401 several hundred requests later, attributed to whatever was running then
// rather than to the refresh that actually failed.
func TestRefreshingTokenStore_failedRefreshDoesNotServeTheExpiredToken(t *testing.T) {
	inner := &recordingStore{stored: domain.OAuthToken{AccessToken: "expired", RefreshToken: "r1", ExpiresAt: at(-1)}}
	s := adapterfortnox.NewRefreshingTokenStore(inner, func(string) (domain.OAuthToken, error) {
		return domain.OAuthToken{}, errors.New("invalid_grant")
	}, 10*time.Minute, func() time.Time { return at(0) }, quietLog())

	_, err := s.Load(context.Background(), "t1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid_grant")
}

// A token with no refresh token cannot be refreshed, and saying so beats a
// token-endpoint error about an empty grant.
func TestRefreshingTokenStore_noRefreshTokenIsAClearError(t *testing.T) {
	inner := &recordingStore{stored: domain.OAuthToken{AccessToken: "expired", ExpiresAt: at(-1)}}
	s := adapterfortnox.NewRefreshingTokenStore(inner, func(string) (domain.OAuthToken, error) {
		t.Fatal("must not call the token endpoint without a refresh token")
		return domain.OAuthToken{}, nil
	}, 10*time.Minute, func() time.Time { return at(0) }, quietLog())

	_, err := s.Load(context.Background(), "t1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no refresh token")
}
