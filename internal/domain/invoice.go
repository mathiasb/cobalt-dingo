package domain

import "fmt"

// SupplierInvoice is the ERP-agnostic domain model for a supplier invoice.
// Amount carries the currency; there is no separate Currency field.
type SupplierInvoice struct {
	InvoiceNumber  int
	SupplierNumber int
	SupplierName   string
	Amount         Money
	DueDate        string
}

// IsForeignCurrency reports whether the invoice is denominated in a non-SEK currency.
func (inv SupplierInvoice) IsForeignCurrency() bool {
	return inv.Amount.Currency != "SEK"
}

// EnrichedInvoice is a FCY invoice with the supplier's payment details attached.
type EnrichedInvoice struct {
	SupplierInvoice
	IBAN string
	BIC  string
}

// SupplierLookup fetches IBAN and BIC for a given supplier number.
// Returns empty strings when the supplier has no IBAN configured.
type SupplierLookup func(supplierNumber int) (iban, bic string, err error)

// Enrich looks up IBAN/BIC for each invoice using lookup and returns only
// invoices where a non-empty IBAN was found. Invoices with missing IBAN are
// silently dropped — the caller should log or report them separately.
func Enrich(invoices []SupplierInvoice, lookup SupplierLookup) ([]EnrichedInvoice, error) {
	var enriched []EnrichedInvoice
	for _, inv := range invoices {
		iban, bic, err := lookup(inv.SupplierNumber)
		if err != nil {
			return nil, fmt.Errorf("lookup supplier %d: %w", inv.SupplierNumber, err)
		}
		if iban == "" {
			continue
		}
		enriched = append(enriched, EnrichedInvoice{
			SupplierInvoice: inv,
			IBAN:            iban,
			BIC:             bic,
		})
	}
	return enriched, nil
}

// Queue holds invoices pending payment processing.
type Queue struct {
	items []SupplierInvoice
}

// Enqueue adds an invoice to the queue.
func (q *Queue) Enqueue(inv SupplierInvoice) {
	q.items = append(q.items, inv)
}

// All returns a snapshot of queued invoices.
func (q *Queue) All() []SupplierInvoice {
	out := make([]SupplierInvoice, len(q.items))
	copy(out, q.items)
	return out
}

// Sync filters invoices from source into queue, keeping only foreign-currency ones.
func Sync(source []SupplierInvoice, queue *Queue) {
	for _, inv := range source {
		if inv.IsForeignCurrency() {
			queue.Enqueue(inv)
		}
	}
}
