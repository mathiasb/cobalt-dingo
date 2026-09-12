package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/mathiasb/cobalt-dingo/internal/adapter/postgres/pgstore"
	"github.com/mathiasb/cobalt-dingo/internal/domain"
)

// TenantRepo implements domain.TenantRepository using PostgreSQL.
type TenantRepo struct{ s *Store }

// NewTenantRepo returns a TenantRepo backed by s.
func NewTenantRepo(s *Store) *TenantRepo { return &TenantRepo{s: s} }

// Get implements domain.TenantRepository.
func (r *TenantRepo) Get(ctx context.Context, id domain.TenantID) (domain.Tenant, error) {
	row, err := r.s.queries.GetTenant(ctx, string(id))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Tenant{}, fmt.Errorf("tenant %s not found", id)
	}
	if err != nil {
		return domain.Tenant{}, fmt.Errorf("get tenant: %w", err)
	}
	return domain.Tenant{ID: domain.TenantID(row.ID), Name: row.Name}, nil
}

// ListByPrefix implements domain.TenantRepository.
//
// LIKE metacharacters in the prefix are escaped, so a tenant id containing `%`
// or `_` cannot widen the match into another user's companies.
func (r *TenantRepo) ListByPrefix(ctx context.Context, prefix string) ([]domain.Tenant, error) {
	rows, err := r.s.queries.ListTenantsByPrefix(ctx, escapeLike(prefix)+"%")
	if err != nil {
		return nil, fmt.Errorf("list tenants by prefix: %w", err)
	}
	out := make([]domain.Tenant, 0, len(rows))
	for _, row := range rows {
		out = append(out, domain.Tenant{ID: domain.TenantID(row.ID), Name: row.Name})
	}
	return out, nil
}

// escapeLike neutralises LIKE wildcards. Postgres LIKE has no metacharacters
// beyond these three, and the default escape character is a backslash.
func escapeLike(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, "%", `\%`)
	return strings.ReplaceAll(s, "_", `\_`)
}

// UpsertTenant implements domain.TenantRepository.
func (r *TenantRepo) UpsertTenant(ctx context.Context, t domain.Tenant) error {
	if err := r.s.queries.UpsertTenant(ctx, pgstore.UpsertTenantParams{
		ID: string(t.ID), Name: t.Name,
	}); err != nil {
		return fmt.Errorf("upsert tenant: %w", err)
	}
	return nil
}

// DefaultDebtorAccount implements domain.TenantRepository.
func (r *TenantRepo) DefaultDebtorAccount(ctx context.Context, tenantID domain.TenantID) (domain.DebtorAccount, error) {
	row, err := r.s.queries.GetDefaultDebtorAccount(ctx, string(tenantID))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.DebtorAccount{}, fmt.Errorf("no default debtor account for tenant %s", tenantID)
	}
	if err != nil {
		return domain.DebtorAccount{}, fmt.Errorf("get default debtor account: %w", err)
	}
	acc := domain.DebtorAccount{
		TenantID:  domain.TenantID(row.TenantID),
		Name:      row.Name,
		IBAN:      row.Iban,
		BIC:       row.Bic,
		IsDefault: row.IsDefault,
	}
	if row.PispHandle.Valid {
		acc.PISPHandle = row.PispHandle.String
	}
	return acc, nil
}
