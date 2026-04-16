package domain

import "time"

// BatchID is the unique identifier for a payment batch.
type BatchID string

// BatchStatus represents the lifecycle state of a payment batch.
type BatchStatus string

// Batch status constants represent the full payment batch lifecycle.
const (
	BatchStatusDraft      BatchStatus = "draft"
	BatchStatusSubmitted  BatchStatus = "submitted"
	BatchStatusConfirmed  BatchStatus = "confirmed"
	BatchStatusReconciled BatchStatus = "reconciled"
)

// Batch is a generated PAIN.001 payment batch.
type Batch struct {
	ID          BatchID
	TenantID    TenantID
	MsgID       string
	Status      BatchStatus
	XML         []byte
	Items       []BatchItem
	CreatedAt   time.Time
	SubmittedAt *time.Time
}

// BatchItem is one invoice payment within a batch.
// ExecutionRate is nil until the bank confirms payment; it is used to calculate
// the FX delta voucher posted back to the ERP.
type BatchItem struct {
	BatchID              BatchID
	FortnoxInvoiceNumber int
	SupplierName         string
	SupplierIBAN         string
	SupplierBIC          string
	Amount               Money
	DueDate              string
	ExecutionRate        *float64 // TODO: replace with exact rational type before FX voucher work
}
