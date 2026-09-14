package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/mathiasb/cobalt-dingo/internal/adapter/postgres/pgstore"
	"github.com/mathiasb/cobalt-dingo/internal/domain"
)

// Compile-time proof the adapter satisfies the port. Without it a signature
// drift shows up only where it is wired, which for a cache is a long way from
// the change that caused it.
var _ domain.VoucherCache = (*VoucherCache)(nil)

// VoucherCache implements domain.VoucherCache using PostgreSQL (ADR-0006).
type VoucherCache struct{ s *Store }

// NewVoucherCache returns a VoucherCache backed by s.
func NewVoucherCache(s *Store) *VoucherCache { return &VoucherCache{s: s} }

// cachedRow is the JSONB shape for one voucher row.
//
// Its own type rather than reusing domain.VoucherRow: Money is a domain type
// whose JSON representation is not part of any contract, and pinning the stored
// shape here means a refactor of Money cannot silently change what is already
// in the database.
type cachedRow struct {
	Account     int    `json:"account"`
	DebitMinor  int64  `json:"debit_minor"`
	CreditMinor int64  `json:"credit_minor"`
	Currency    string `json:"currency"`
	Description string `json:"description"`
	CostCenter  string `json:"cost_center"`
	Project     string `json:"project"`
}

// LoadYear implements domain.VoucherCache.
//
// A missing sync row yields the zero time, which Assess treats as stale
// however many vouchers are present — a cache with rows and no record of
// having been verified is exactly what ADR-0006 refuses to serve.
func (c *VoucherCache) LoadYear(ctx context.Context, tenantID domain.TenantID, yearID int) ([]domain.Voucher, time.Time, error) {
	rows, err := c.s.queries.ListCachedVouchers(ctx, pgstore.ListCachedVouchersParams{
		TenantID: string(tenantID),
		YearID:   int32(yearID),
	})
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("list cached vouchers: %w", err)
	}

	out := make([]domain.Voucher, 0, len(rows))
	for _, r := range rows {
		var stored []cachedRow
		if err := json.Unmarshal(r.Rows, &stored); err != nil {
			// Refuse rather than skip. A voucher whose rows will not decode is
			// a corrupt cache entry, and dropping it returns a short set that
			// looks complete — the failure this whole design exists to remove.
			return nil, time.Time{}, fmt.Errorf("decode cached rows for voucher %s/%d: %w", r.Series, r.Number, err)
		}
		v := domain.Voucher{
			Series:          r.Series,
			Number:          int(r.Number),
			Description:     r.Description,
			TransactionDate: r.TransactionDate.Format("2006-01-02"),
			Year:            yearID,
			Rows:            make([]domain.VoucherRow, 0, len(stored)),
		}
		for _, sr := range stored {
			v.Rows = append(v.Rows, domain.VoucherRow{
				Account:     sr.Account,
				Debit:       domain.Money{MinorUnits: sr.DebitMinor, Currency: sr.Currency},
				Credit:      domain.Money{MinorUnits: sr.CreditMinor, Currency: sr.Currency},
				Description: sr.Description,
				CostCenter:  sr.CostCenter,
				Project:     sr.Project,
			})
		}
		out = append(out, v)
	}

	sync, err := c.s.queries.GetVoucherSync(ctx, pgstore.GetVoucherSyncParams{
		TenantID: string(tenantID),
		YearID:   int32(yearID),
	})
	if errors.Is(err, sql.ErrNoRows) {
		return out, time.Time{}, nil
	}
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("get voucher sync: %w", err)
	}
	return out, sync.SyncedAt, nil
}

// SaveYear implements domain.VoucherCache.
//
// The delete and the writes run in ONE transaction. Without it a failure
// part-way through would leave a year holding some vouchers and a sync row
// claiming completeness — a cache that lies about itself, which is worse than
// no cache. The sync row is written last, inside the same transaction, so it
// can never describe a set that was not fully written.
func (c *VoucherCache) SaveYear(
	ctx context.Context,
	tenantID domain.TenantID,
	yearID int,
	vouchers []domain.Voucher,
	remoteTotal int,
	syncedAt time.Time,
) error {
	tx, err := c.s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin voucher cache write: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // no-op once committed

	q := c.s.queries.WithTx(tx)

	if err := q.DeleteCachedYear(ctx, pgstore.DeleteCachedYearParams{
		TenantID: string(tenantID), YearID: int32(yearID),
	}); err != nil {
		return fmt.Errorf("clear cached year: %w", err)
	}

	for _, v := range vouchers {
		date, err := time.Parse("2006-01-02", v.TransactionDate)
		if err != nil {
			return fmt.Errorf("voucher %s/%d has an unparseable date %q: %w", v.Series, v.Number, v.TransactionDate, err)
		}
		stored := make([]cachedRow, 0, len(v.Rows))
		for _, r := range v.Rows {
			stored = append(stored, cachedRow{
				Account:     r.Account,
				DebitMinor:  r.Debit.MinorUnits,
				CreditMinor: r.Credit.MinorUnits,
				Currency:    r.Debit.Currency,
				Description: r.Description,
				CostCenter:  r.CostCenter,
				Project:     r.Project,
			})
		}
		raw, err := json.Marshal(stored)
		if err != nil {
			return fmt.Errorf("encode rows for voucher %s/%d: %w", v.Series, v.Number, err)
		}
		if err := q.UpsertCachedVoucher(ctx, pgstore.UpsertCachedVoucherParams{
			TenantID:        string(tenantID),
			YearID:          int32(yearID),
			Series:          v.Series,
			Number:          int32(v.Number),
			TransactionDate: date,
			Description:     v.Description,
			Rows:            raw,
		}); err != nil {
			return fmt.Errorf("cache voucher %s/%d: %w", v.Series, v.Number, err)
		}
	}

	if err := q.UpsertVoucherSync(ctx, pgstore.UpsertVoucherSyncParams{
		TenantID:    string(tenantID),
		YearID:      int32(yearID),
		SyncedAt:    syncedAt,
		RemoteTotal: int32(remoteTotal),
	}); err != nil {
		return fmt.Errorf("record voucher sync: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit voucher cache: %w", err)
	}
	return nil
}
