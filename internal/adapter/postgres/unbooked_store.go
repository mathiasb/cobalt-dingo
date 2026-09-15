package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/mathiasb/cobalt-dingo/internal/adapter/postgres/pgstore"
	"github.com/mathiasb/cobalt-dingo/internal/domain"
)

var _ domain.UnbookedStore = (*UnbookedStore)(nil)

// UnbookedStore implements domain.UnbookedStore using PostgreSQL (#93).
type UnbookedStore struct{ s *Store }

// NewUnbookedStore returns an UnbookedStore backed by s.
func NewUnbookedStore(s *Store) *UnbookedStore { return &UnbookedStore{s: s} }

// Seen implements domain.UnbookedStore.
func (u *UnbookedStore) Seen(ctx context.Context, tenantID domain.TenantID) ([]domain.UnbookedRef, error) {
	rows, err := u.s.queries.ListUnbookedSeen(ctx, string(tenantID))
	if err != nil {
		return nil, fmt.Errorf("list remembered unbooked invoices: %w", err)
	}
	out := make([]domain.UnbookedRef, 0, len(rows))
	for _, r := range rows {
		out = append(out, domain.UnbookedRef{
			Kind:          domain.UnbookedKind(r.Kind),
			InvoiceNumber: int(r.InvoiceNumber),
		})
	}
	return out, nil
}

// Reconcile implements domain.UnbookedStore.
//
// One transaction. A partial reconcile could persist an appearance without
// forgetting a resolution, or worse, record an appearance whose alert then
// failed to send — leaving an obligation marked "already reported" that nobody
// was ever told about. That is the one outcome an alerting store must not
// produce, so the caller sends FIRST and reconciles after.
func (u *UnbookedStore) Reconcile(ctx context.Context, tenantID domain.TenantID, change domain.UnbookedChange) error {
	tx, err := u.s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin unbooked reconcile: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // no-op once committed

	q := u.s.queries.WithTx(tx)
	now := time.Now()

	for _, r := range change.Appeared {
		if err := q.RememberUnbooked(ctx, pgstore.RememberUnbookedParams{
			TenantID:      string(tenantID),
			Kind:          string(r.Kind),
			InvoiceNumber: int32(r.InvoiceNumber),
			FirstSeenAt:   now,
		}); err != nil {
			return fmt.Errorf("remember %s: %w", r, err)
		}
	}
	for _, r := range change.Resolved {
		if err := q.ForgetUnbooked(ctx, pgstore.ForgetUnbookedParams{
			TenantID:      string(tenantID),
			Kind:          string(r.Kind),
			InvoiceNumber: int32(r.InvoiceNumber),
		}); err != nil {
			return fmt.Errorf("forget %s: %w", r, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit unbooked reconcile: %w", err)
	}
	return nil
}
