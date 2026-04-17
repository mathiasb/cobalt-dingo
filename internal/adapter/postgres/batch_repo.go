package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/mathiasb/cobalt-dingo/internal/adapter/postgres/pgstore"
	"github.com/mathiasb/cobalt-dingo/internal/domain"
)

// BatchRepo implements domain.BatchRepository using PostgreSQL.
type BatchRepo struct{ s *Store }

// NewBatchRepo returns a BatchRepo backed by s.
func NewBatchRepo(s *Store) *BatchRepo { return &BatchRepo{s: s} }

// Save implements domain.BatchRepository.
// Inserts the batch and all items in a single transaction. Generates UUIDs
// for any items that don't already carry an ID.
func (r *BatchRepo) Save(ctx context.Context, b domain.Batch) error {
	tx, err := r.s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	q := r.s.queries.WithTx(tx)

	batchID := string(b.ID)
	if batchID == "" {
		batchID = newUUID()
	}

	if _, err := q.InsertBatch(ctx, pgstore.InsertBatchParams{
		ID:       batchID,
		TenantID: string(b.TenantID),
		MsgID:    b.MsgID,
		Status:   string(b.Status),
		Xml:      b.XML,
	}); err != nil {
		return fmt.Errorf("insert batch: %w", err)
	}

	for _, item := range b.Items {
		itemID := newUUID()
		dueDate, err := time.Parse("2006-01-02", item.DueDate)
		if err != nil {
			return fmt.Errorf("parse due_date %q: %w", item.DueDate, err)
		}
		if err := q.InsertBatchItem(ctx, pgstore.InsertBatchItemParams{
			ID:                   itemID,
			BatchID:              batchID,
			FortnoxInvoiceNumber: int32(item.FortnoxInvoiceNumber),
			SupplierName:         item.SupplierName,
			SupplierIban:         item.SupplierIBAN,
			SupplierBic:          item.SupplierBIC,
			Currency:             item.Amount.Currency,
			AmountMinorUnits:     item.Amount.MinorUnits,
			DueDate:              dueDate,
		}); err != nil {
			return fmt.Errorf("insert batch item %d: %w", item.FortnoxInvoiceNumber, err)
		}
	}

	return tx.Commit()
}

// Get implements domain.BatchRepository.
func (r *BatchRepo) Get(ctx context.Context, tenantID domain.TenantID, id domain.BatchID) (domain.Batch, error) {
	row, err := r.s.queries.GetBatch(ctx, pgstore.GetBatchParams{
		TenantID: string(tenantID),
		ID:       string(id),
	})
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Batch{}, fmt.Errorf("batch %s not found", id)
	}
	if err != nil {
		return domain.Batch{}, fmt.Errorf("get batch: %w", err)
	}

	items, err := r.s.queries.GetBatchItems(ctx, string(id))
	if err != nil {
		return domain.Batch{}, fmt.Errorf("get batch items: %w", err)
	}

	return toDomainBatch(row, items), nil
}

// List implements domain.BatchRepository.
func (r *BatchRepo) List(ctx context.Context, tenantID domain.TenantID) ([]domain.Batch, error) {
	rows, err := r.s.queries.ListBatches(ctx, string(tenantID))
	if err != nil {
		return nil, fmt.Errorf("list batches: %w", err)
	}
	batches := make([]domain.Batch, len(rows))
	for i, row := range rows {
		batches[i] = toDomainBatch(row, nil) // items not loaded for list view
	}
	return batches, nil
}

func toDomainBatch(row pgstore.PaymentBatch, items []pgstore.BatchItem) domain.Batch {
	b := domain.Batch{
		ID:        domain.BatchID(row.ID),
		TenantID:  domain.TenantID(row.TenantID),
		MsgID:     row.MsgID,
		Status:    toDomainBatchStatus(row.Status),
		XML:       row.Xml,
		CreatedAt: row.CreatedAt,
	}
	if row.SubmittedAt.Valid {
		t := row.SubmittedAt.Time
		b.SubmittedAt = &t
	}
	for _, it := range items {
		b.Items = append(b.Items, toDomainBatchItem(it))
	}
	return b
}

func toDomainBatchItem(it pgstore.BatchItem) domain.BatchItem {
	item := domain.BatchItem{
		BatchID:              domain.BatchID(it.BatchID),
		FortnoxInvoiceNumber: int(it.FortnoxInvoiceNumber),
		SupplierName:         it.SupplierName,
		SupplierIBAN:         it.SupplierIban,
		SupplierBIC:          it.SupplierBic,
		Amount:               domain.Money{MinorUnits: it.AmountMinorUnits, Currency: it.Currency},
		DueDate:              it.DueDate.Format("2006-01-02"),
	}
	if it.ExecutionRate.Valid {
		if f, err := strconv.ParseFloat(it.ExecutionRate.String, 64); err == nil {
			item.ExecutionRate = &f
		}
	}
	return item
}
