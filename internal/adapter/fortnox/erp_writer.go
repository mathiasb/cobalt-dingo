package fortnox

import (
	"context"
	"fmt"

	"github.com/mathiasb/cobalt-dingo/internal/domain"
	"github.com/mathiasb/cobalt-dingo/internal/fortnox"
)

// ERPWriter implements domain.ERPWriter by recording supplier invoice payments
// and posting them to the Fortnox GL via the bookkeep action.
// Each call results in two Fortnox API requests: POST payment + PUT bookkeep.
type ERPWriter struct {
	connector *Connector
}

// NewERPWriter returns an ERPWriter backed by the given Connector.
func NewERPWriter(connector *Connector) *ERPWriter {
	return &ERPWriter{connector: connector}
}

// RecordAndBookkeep implements domain.ERPWriter.
// It posts the payment at the execution rate, then immediately calls the
// bookkeep action to post the GL voucher. The FX delta between the invoice
// rate and execution rate is recorded by Fortnox automatically when the
// CurrencyRate differs from the original invoice rate.
func (w *ERPWriter) RecordAndBookkeep(ctx context.Context, tenantID domain.TenantID, item domain.BatchItem, executionRate float64, paymentDate string) error {
	client, err := w.connector.client(ctx, tenantID)
	if err != nil {
		return fmt.Errorf("erp writer: %w", err)
	}

	paymentNumber, err := client.RecordPayment(fortnox.SupplierInvoicePayment{
		InvoiceNumber: item.FortnoxInvoiceNumber,
		Amount:        item.Amount.Float(),
		CurrencyRate:  executionRate,
		PaymentDate:   paymentDate,
	})
	if err != nil {
		return fmt.Errorf("record payment invoice %d: %w", item.FortnoxInvoiceNumber, err)
	}

	if err := client.BookkeepPayment(paymentNumber); err != nil {
		return fmt.Errorf("bookkeep payment %d (invoice %d): %w", paymentNumber, item.FortnoxInvoiceNumber, err)
	}

	return nil
}
