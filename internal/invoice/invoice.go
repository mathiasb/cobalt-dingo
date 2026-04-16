// Package invoice contains the domain model and sync logic for supplier invoices.
package invoice

// SupplierInvoice is the ERP-agnostic domain model for a supplier invoice.
type SupplierInvoice struct {
	InvoiceNumber int
	Currency      string
	Total         float64
	DueDate       string
}

// IsForeignCurrency reports whether the invoice is denominated in a non-SEK currency.
func (inv SupplierInvoice) IsForeignCurrency() bool {
	return inv.Currency != "SEK"
}
