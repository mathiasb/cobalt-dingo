package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

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
