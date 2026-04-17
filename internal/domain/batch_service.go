package domain

import (
	"context"
	"fmt"
	"time"
)

// ERPWriter writes payment execution results back to the ERP.
// Implementations: internal/adapter/fortnox (writes payment + bookkeep).
type ERPWriter interface {
	RecordAndBookkeep(ctx context.Context, tenantID TenantID, item BatchItem, executionRate float64, paymentDate string) error
}

// BatchService orchestrates batch creation, PISP submission, and ERP write-back.
// It is the primary entry point for the payment pipeline beyond XML generation.
type BatchService struct {
	batches   BatchRepository
	tenants   TenantRepository
	submitter PaymentSubmitter
	erp       ERPWriter // may be nil when ERP write-back is not configured
}

// NewBatchService constructs a BatchService. erp may be nil to disable
// automatic write-back (useful in tests and early deployments).
func NewBatchService(batches BatchRepository, tenants TenantRepository, submitter PaymentSubmitter, erp ERPWriter) *BatchService {
	return &BatchService{batches: batches, tenants: tenants, submitter: submitter, erp: erp}
}

// SaveDraft persists a newly generated batch in draft status.
// The batch ID is assigned by the repository on insert; callers should treat b
// as immutable after this call.
func (svc *BatchService) SaveDraft(ctx context.Context, tenantID TenantID, msgID string, items []BatchItem, xmlBytes []byte) (Batch, error) {
	b := Batch{
		TenantID:  tenantID,
		MsgID:     msgID,
		Status:    BatchStatusDraft,
		XML:       xmlBytes,
		Items:     items,
		CreatedAt: time.Now().UTC(),
	}
	if err := svc.batches.Save(ctx, b); err != nil {
		return Batch{}, fmt.Errorf("save draft batch: %w", err)
	}
	return b, nil
}

// Submit moves a draft batch to submitted status and calls the PISP.
// Returns the bank's submission reference on success.
func (svc *BatchService) Submit(ctx context.Context, tenantID TenantID, batchID BatchID) (SubmissionRef, error) {
	b, err := svc.batches.Get(ctx, tenantID, batchID)
	if err != nil {
		return "", fmt.Errorf("get batch: %w", err)
	}
	if b.Status != BatchStatusDraft {
		return "", fmt.Errorf("batch %s is %s, expected draft", batchID, b.Status)
	}

	account, err := svc.tenants.DefaultDebtorAccount(ctx, tenantID)
	if err != nil {
		return "", fmt.Errorf("get debtor account: %w", err)
	}

	ref, err := svc.submitter.Submit(ctx, b, account)
	if err != nil {
		return "", fmt.Errorf("pisp submit: %w", err)
	}

	now := time.Now().UTC()
	b.Status = BatchStatusSubmitted
	b.SubmittedAt = &now
	if saveErr := svc.batches.Save(ctx, b); saveErr != nil {
		// Log but don't fail — the payment was submitted. Reconcile on next poll.
		_ = saveErr
	}

	return ref, nil
}

// ExecutionConfirmation carries bank confirmation data for a single invoice payment.
type ExecutionConfirmation struct {
	FortnoxInvoiceNumber int
	ExecutionRate        float64 // SEK per 1 FCY unit at execution
	PaymentDate          string  // YYYY-MM-DD
}

// ConfirmExecution marks a submitted batch as confirmed and, for each invoice:
//  1. Records the execution rate on the batch item.
//  2. If an ERPWriter is configured, calls RecordAndBookkeep to write the
//     payment voucher back to the ERP and calculate the FX delta.
//
// Partial failures are collected and returned as a combined error; confirmed
// items are persisted even when some fail.
func (svc *BatchService) ConfirmExecution(ctx context.Context, tenantID TenantID, batchID BatchID, confirmations []ExecutionConfirmation) error {
	b, err := svc.batches.Get(ctx, tenantID, batchID)
	if err != nil {
		return fmt.Errorf("get batch: %w", err)
	}
	if b.Status != BatchStatusSubmitted {
		return fmt.Errorf("batch %s is %s, expected submitted", batchID, b.Status)
	}

	// Index items for quick lookup.
	itemByInvoice := make(map[int]BatchItem, len(b.Items))
	for _, item := range b.Items {
		itemByInvoice[item.FortnoxInvoiceNumber] = item
	}

	var errs []error
	for _, conf := range confirmations {
		item, ok := itemByInvoice[conf.FortnoxInvoiceNumber]
		if !ok {
			errs = append(errs, fmt.Errorf("invoice %d not in batch %s", conf.FortnoxInvoiceNumber, batchID))
			continue
		}
		rate := conf.ExecutionRate
		item.ExecutionRate = &rate

		if svc.erp != nil {
			if erpErr := svc.erp.RecordAndBookkeep(ctx, tenantID, item, conf.ExecutionRate, conf.PaymentDate); erpErr != nil {
				errs = append(errs, fmt.Errorf("erp write-back invoice %d: %w", conf.FortnoxInvoiceNumber, erpErr))
			}
		}
	}

	b.Status = BatchStatusConfirmed
	if saveErr := svc.batches.Save(ctx, b); saveErr != nil {
		errs = append(errs, fmt.Errorf("save confirmed batch: %w", saveErr))
	}

	if len(errs) > 0 {
		return fmt.Errorf("confirm execution partial failure (%d errors): %w", len(errs), errs[0])
	}
	return nil
}
