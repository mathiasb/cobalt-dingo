package invoice

import "fmt"

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
			continue // skip — no IBAN on file
		}
		enriched = append(enriched, EnrichedInvoice{
			SupplierInvoice: inv,
			IBAN:            iban,
			BIC:             bic,
		})
	}
	return enriched, nil
}
