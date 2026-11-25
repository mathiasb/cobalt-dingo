package extract

import (
	"fmt"
	"math/big"
)

// reconcileTolerance is the largest gap, in minor units, accepted between
// net + VAT and the total. Swedish invoices round to whole kronor as a
// separate line (öresavrundning, at most 50 öre), so a smaller gap is normal.
const reconcileTolerance = 50

// Reconciled reports whether net + VAT equals the total within the rounding
// tolerance. False when the net total was not read: with nothing to check
// against, the total is unproven, not fine.
//
// This catches a misread of the total alone. It does not catch the net, VAT and
// total being misread consistently, and it has not been measured against that.
func (i Invoice) Reconciled() bool {
	if !i.NetKnown {
		return false
	}
	// Big integers: an Invoice is a plain struct, and a hand-built one with
	// amounts near int64's edge must not wrap into a small gap.
	gap := new(big.Int).Add(big.NewInt(i.NetTotal), big.NewInt(i.VATAmount))
	gap.Sub(gap, big.NewInt(i.Total))
	gap.Abs(gap)
	return gap.Cmp(big.NewInt(reconcileTolerance)) <= 0
}

// Warnings lists what a reader should check before trusting this invoice.
// The reconciliation problem, if any, comes first.
func (i Invoice) Warnings() []string {
	var w []string
	switch {
	case !i.NetKnown:
		w = append(w, "net total was not read, so the total cannot be cross-checked")
	case !i.Reconciled():
		w = append(w, fmt.Sprintf("net %s + VAT %s does not equal total %s",
			minor(i.NetTotal), minor(i.VATAmount), minor(i.Total)))
	}
	if i.InvoiceDateAmbiguous {
		w = append(w, fmt.Sprintf("invoice date could be read two ways; read as %s", i.InvoiceDate))
	}
	if i.DueDateAmbiguous {
		w = append(w, fmt.Sprintf("due date could be read two ways; read as %s", i.DueDate))
	}
	return w
}

func minor(v int64) string { return fmt.Sprintf("%d.%02d", v/100, v%100) }
