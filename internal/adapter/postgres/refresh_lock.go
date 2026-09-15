package postgres

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/mathiasb/cobalt-dingo/internal/domain"
)

var _ domain.RefreshLock = (*RefreshLock)(nil)

// RefreshLock serialises token refreshes using a PostgreSQL advisory lock.
//
// Advisory rather than a row lock: the critical section spans an HTTP call to
// Fortnox's token endpoint, and holding a row lock across a network round trip
// would block anything touching that row. An advisory lock guards the
// OPERATION, which is what needs guarding.
//
// Session-scoped (pg_advisory_lock, not the _xact_ variant) because there is
// no transaction here — the work being serialised is an HTTP request. It is
// held on one dedicated connection and released explicitly.
type RefreshLock struct {
	s   *Store
	log *slog.Logger
}

// NewRefreshLock returns a RefreshLock backed by s.
func NewRefreshLock(s *Store, log *slog.Logger) *RefreshLock {
	return &RefreshLock{s: s, log: log}
}

// Acquire implements domain.RefreshLock. It BLOCKS until the lock is free.
//
// Blocking is correct here. The alternative — returning "busy" — sends the
// caller away without a usable token, when waiting a second for the other
// process to finish yields a perfectly good one. The re-read that follows in
// RefreshingTokenStore is what makes the wait worthwhile.
func (l *RefreshLock) Acquire(ctx context.Context, tenantID domain.TenantID) (func(), error) {
	// A dedicated connection: an advisory lock belongs to the session that
	// took it, and a pooled query could release it on a different one.
	conn, err := l.s.db.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("connection for refresh lock: %w", err)
	}

	// hashtext maps the tenant ID to the bigint the advisory lock API wants.
	// A collision between two tenants would only mean one waits for the
	// other — slower, never wrong.
	if _, err := conn.ExecContext(ctx, `SELECT pg_advisory_lock(hashtext($1))`, string(tenantID)); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("acquire refresh lock: %w", err)
	}

	return func() {
		// Unlock on the same connection, then return it. Closing without
		// unlocking would hold the lock until the connection is reaped.
		if _, err := conn.ExecContext(context.WithoutCancel(ctx),
			`SELECT pg_advisory_unlock(hashtext($1))`, string(tenantID)); err != nil {
			// Logged, not returned: the caller is already past the point where
			// it could act, and a leaked advisory lock is released when the
			// connection closes.
			l.log.Error("release refresh lock", "tenant", tenantID, "err", err)
		}
		_ = conn.Close()
	}, nil
}
