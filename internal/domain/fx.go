package domain

import "fmt"

// FXDelta is the gain or loss arising from the difference between the invoice
// exchange rate and the actual execution rate at payment date.
// Positive = gain (BAS 3960), Negative = loss (BAS 7960).
// Value is in SEK minor units (öre).
type FXDelta struct {
	InvoiceNumber int
	SupplierName  string
	Currency      string
	AmountMinor   int64   // foreign-currency amount in minor units
	InvoiceRate   float64 // SEK per 1 FCY at invoice creation
	ExecutionRate float64 // SEK per 1 FCY at payment execution
	DeltaSEKMinor int64   // (executionRate − invoiceRate) × amountMinor, rounded
}

// IsGain reports whether the FX movement favoured the payer (we paid less SEK
// than the invoice implied).
func (d FXDelta) IsGain() bool { return d.DeltaSEKMinor > 0 }

// CalculateFXDelta computes the SEK FX delta for a batch item after execution.
// invoiceRate and executionRate must both be SEK per 1 unit of the foreign currency.
// Returns an error if either rate is zero (would indicate a data problem).
func CalculateFXDelta(item BatchItem, invoiceRate, executionRate float64) (FXDelta, error) {
	if invoiceRate <= 0 {
		return FXDelta{}, fmt.Errorf("invoice rate must be positive, got %v", invoiceRate)
	}
	if executionRate <= 0 {
		return FXDelta{}, fmt.Errorf("execution rate must be positive, got %v", executionRate)
	}
	// Delta in FCY minor units × (executionRate − invoiceRate) / 100
	// Both rates are per 1 FCY unit, amounts are in minor units (×100).
	// Result is in SEK minor units.
	deltaPerUnit := executionRate - invoiceRate
	deltaSEK := float64(item.Amount.MinorUnits) * deltaPerUnit / 100.0
	deltaSEKMinor := int64(deltaSEK * 100)

	return FXDelta{
		InvoiceNumber: item.FortnoxInvoiceNumber,
		SupplierName:  item.SupplierName,
		Currency:      item.Amount.Currency,
		AmountMinor:   item.Amount.MinorUnits,
		InvoiceRate:   invoiceRate,
		ExecutionRate: executionRate,
		DeltaSEKMinor: deltaSEKMinor,
	}, nil
}
