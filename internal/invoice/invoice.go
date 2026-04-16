// Package invoice contains the domain model and sync logic for supplier invoices.
package invoice

// SupplierInvoice is the ERP-agnostic domain model for a supplier invoice.
type SupplierInvoice struct {
	InvoiceNumber  int
	SupplierNumber int
	SupplierName   string
	Currency       string
	Total          float64
	DueDate        string
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

// IsForeignCurrency reports whether the invoice is denominated in a non-SEK currency.
func (inv SupplierInvoice) IsForeignCurrency() bool {
	return inv.Currency != "SEK"
}
