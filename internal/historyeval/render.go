package historyeval

import (
	"fmt"
	"strings"

	"github.com/mathiasb/cobalt-dingo/internal/booking"
)

// Render prints the report as text for a Job log. It prints what Report holds
// and nothing else, so it cannot print a supplier, a number or an amount.
func Render(r Report) string {
	var b strings.Builder
	line := func(format string, a ...any) { fmt.Fprintf(&b, format+"\n", a...) }

	line("HISTORY EVALUATION (aggregates only: no names, numbers, amounts or accounts are printed)")
	line("")
	line("INVOICES: invoices: %d, dated %s to %s", r.Invoices.Total, dash(r.Invoices.FirstDate), dash(r.Invoices.LastDate))
	line("  booked: %s", pct(r.Invoices.Booked, r.Invoices.Total))
	line("  cancelled: %s", pct(r.Invoices.Cancelled, r.Invoices.Total))
	line("  with rows: %s", pct(r.Invoices.WithRows, r.Invoices.Total))
	line("  with voucher link: %s", pct(r.Invoices.WithVoucherLink, r.Invoices.Total))
	line("  with cost centre or project on a row: %s", pct(r.Invoices.WithCostCenterOrProject, r.Invoices.Total))
	line("  not in SEK: %s", pct(r.Invoices.NonSEK, r.Invoices.Total))
	line("")
	line("SUPPLIERS: suppliers: %d", r.Suppliers.Total)
	line("  organisation number: %s", pct(r.Suppliers.WithOrganisationNumber, r.Suppliers.Total))
	line("  VAT number: %s", pct(r.Suppliers.WithVATNumber, r.Suppliers.Total))
	line("  bank account number: %s", pct(r.Suppliers.WithBankAccount, r.Suppliers.Total))
	line("  IBAN: %s", pct(r.Suppliers.WithIBAN, r.Suppliers.Total))
	line("  predefined account: %s", pct(r.Suppliers.WithPredefinedAccount, r.Suppliers.Total))
	line("  invoiced suppliers booked to more than one account: %s", pct(r.Diversity.WithMoreThanOneAccount, r.Diversity.Suppliers))
	line("")
	line("EVALUATION: usable invoices %d (no derivable expense account: %d, cancelled and skipped: %d); trained on %d", r.Eval.Usable, r.Eval.NoAccount, r.Eval.CancelledSkipped, r.Eval.Train)
	line("held-out = the latest invoices by date, never a random split.")
	result := func(name string, res booking.Result) {
		line("  %s: tested %d, top-1 %s, top-3 %s, unseen suppliers %d", name, res.Test, pct(res.Top1, res.Test), pct(res.Top3, res.Test), res.Unseen)
	}
	result("keyed by the ERP's supplier number", r.ByNumber)
	result("keyed by supplier name (the demo's method)", r.ByName)
	line("  the supplier's predefined account as a baseline: %s of the %d test invoices whose supplier has one", pct(r.Predefined.Right, r.Predefined.Covered), r.Predefined.Covered)
	line("")
	line("A held-out rate is a measurement on this company's past, not a guarantee about the next invoice.")
	return b.String()
}

func pct(n, d int) string {
	if d <= 0 {
		return "n/a"
	}
	return fmt.Sprintf("%.1f%% (%d/%d)", 100*float64(n)/float64(d), n, d)
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
