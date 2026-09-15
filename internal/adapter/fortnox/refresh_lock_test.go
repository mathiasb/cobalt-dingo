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

// fakeLock records the order of operations, which is the whole point: the
// reload MUST happen after the lock is held.
type fakeLock struct {
	events   *[]string
	acquired int
	released int
	err      error
}

func (l *fakeLock) Acquire(context.Context, domain.TenantID) (func(), error) {
	if l.err != nil {
		return nil, l.err
	}
	l.acquired++
	*l.events = append(*l.events, "acquire")
	return func() {
		l.released++
		*l.events = append(*l.events, "release")
	}, nil
}

type recordingStore2 struct {
	tokens  []domain.OAuthToken // returned in order, one per Load
	loads   int
	events  *[]string
	refresh int
}

func (s *recordingStore2) Load(context.Context, domain.TenantID) (domain.OAuthToken, error) {
	*s.events = append(*s.events, "load")
	i := s.loads
	s.loads++
	if i >= len(s.tokens) {
		i = len(s.tokens) - 1
	}
	return s.tokens[i], nil
}

func (s *recordingStore2) Save(context.Context, domain.TenantID, domain.OAuthToken) error {
	return nil
}

func (s *recordingStore2) AtomicRefresh(context.Context, domain.TenantID, domain.OAuthToken, domain.OAuthToken) error {
	s.refresh++
	return nil
}

func (s *recordingStore2) Delete(context.Context, domain.TenantID) error { return nil }

func expired() domain.OAuthToken {
	return domain.OAuthToken{AccessToken: "old", RefreshToken: "RT1", ExpiresAt: time.Now().Add(-time.Minute)}
}

func healthy() domain.OAuthToken {
	return domain.OAuthToken{AccessToken: "someone-elses", RefreshToken: "RT2", ExpiresAt: time.Now().Add(time.Hour)}
}

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// THE defect this fixes, reproduced. On 2026-09-14 three overview Jobs started
// in the same second, all loaded the same token, all found it expired, and all
// POSTed the SAME refresh token to Fortnox before any had stored a
// replacement. Fortnox rotates refresh tokens and detects reuse, so it
// invalidated the whole family and the production connection had to be
// re-authorized by hand.
//
// AtomicRefresh's compare-and-swap cannot prevent it: the CAS runs AFTER the
// HTTP call, by which time the reuse has already happened. Serialising before
// the network call is the only fix.
//
// So: acquire the lock, then RE-READ. If another process refreshed while we
// waited, its token is already good and we must not call the token endpoint at
// all.
func TestRefreshingTokenStore_rereadsUnderTheLockAndSkipsTheRefresh(t *testing.T) {
	var events []string
	// First Load sees the expired token; the second (under the lock) sees the
	// one another process just stored.
	store := &recordingStore2{tokens: []domain.OAuthToken{expired(), healthy()}, events: &events}
	lock := &fakeLock{events: &events}

	var refreshCalls int
	s := adapterfortnox.NewRefreshingTokenStore(store, func(string) (domain.OAuthToken, error) {
		refreshCalls++
		return domain.OAuthToken{}, errors.New("must not be called — another process already refreshed")
	}, 10*time.Minute, time.Now, quiet()).WithRefreshLock(lock)

	tok, err := s.Load(context.Background(), "t1")

	require.NoError(t, err)
	assert.Equal(t, "someone-elses", tok.AccessToken, "the winner's token must be used")
	assert.Zero(t, refreshCalls, "no second call to the token endpoint with a consumed refresh token")
	assert.Equal(t, []string{"load", "acquire", "load", "release"}, events,
		"the re-read must happen AFTER the lock is acquired")
	assert.Equal(t, 1, lock.released)
}

// When the re-read still shows an expired token, this process is the winner
// and does refresh — once, under the lock.
func TestRefreshingTokenStore_refreshesWhenStillStaleUnderTheLock(t *testing.T) {
	var events []string
	store := &recordingStore2{tokens: []domain.OAuthToken{expired(), expired()}, events: &events}
	lock := &fakeLock{events: &events}

	var refreshCalls int
	s := adapterfortnox.NewRefreshingTokenStore(store, func(rt string) (domain.OAuthToken, error) {
		refreshCalls++
		assert.Equal(t, "RT1", rt)
		return domain.OAuthToken{AccessToken: "fresh", RefreshToken: "RT9", ExpiresAt: time.Now().Add(time.Hour)}, nil
	}, 10*time.Minute, time.Now, quiet()).WithRefreshLock(lock)

	tok, err := s.Load(context.Background(), "t1")

	require.NoError(t, err)
	assert.Equal(t, "fresh", tok.AccessToken)
	assert.Equal(t, 1, refreshCalls)
	assert.Equal(t, 1, store.refresh)
	assert.Equal(t, 1, lock.released, "the lock must be released after a successful refresh")
}

// The lock must be released even when the refresh fails, or the next process
// waits forever on a token nobody is refreshing.
func TestRefreshingTokenStore_releasesTheLockWhenRefreshFails(t *testing.T) {
	var events []string
	store := &recordingStore2{tokens: []domain.OAuthToken{expired(), expired()}, events: &events}
	lock := &fakeLock{events: &events}

	s := adapterfortnox.NewRefreshingTokenStore(store, func(string) (domain.OAuthToken, error) {
		return domain.OAuthToken{}, errors.New("invalid_grant")
	}, 10*time.Minute, time.Now, quiet()).WithRefreshLock(lock)

	_, err := s.Load(context.Background(), "t1")

	require.Error(t, err)
	assert.Equal(t, 1, lock.released)
}

// An unavailable lock must NOT fall through to an unserialised refresh: that
// is the exact behaviour that destroyed the token. Fail instead — a failed
// read is recoverable, a revoked token needs a human at a consent screen.
func TestRefreshingTokenStore_refusesToRefreshWithoutTheLock(t *testing.T) {
	var events []string
	store := &recordingStore2{tokens: []domain.OAuthToken{expired()}, events: &events}
	lock := &fakeLock{events: &events, err: errors.New("postgres unreachable")}

	var refreshCalls int
	s := adapterfortnox.NewRefreshingTokenStore(store, func(string) (domain.OAuthToken, error) {
		refreshCalls++
		return domain.OAuthToken{}, nil
	}, 10*time.Minute, time.Now, quiet()).WithRefreshLock(lock)

	_, err := s.Load(context.Background(), "t1")

	require.Error(t, err)
	assert.Zero(t, refreshCalls, "refreshing without serialisation is what caused the outage")
}

// A healthy token must not touch the lock at all — the hot path stays free.
func TestRefreshingTokenStore_healthyTokenNeverLocks(t *testing.T) {
	var events []string
	store := &recordingStore2{tokens: []domain.OAuthToken{healthy()}, events: &events}
	lock := &fakeLock{events: &events}

	_, err := adapterfortnox.NewRefreshingTokenStore(store, nil, 10*time.Minute, time.Now, quiet()).
		WithRefreshLock(lock).Load(context.Background(), "t1")

	require.NoError(t, err)
	assert.Zero(t, lock.acquired)
}
