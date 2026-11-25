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
	line("Counts below 3 are printed as <3. This reduces what a reader can pick out; it is not anonymisation: a total and its parts can still subtract to a suppressed count.")
	line("INVOICES: invoices: %s, dated %s to %s", cnt(r.Invoices.Total), dash(r.Invoices.FirstDate), dash(r.Invoices.LastDate))
	line("  booked: %s", pct(r.Invoices.Booked, r.Invoices.Total))
	line("  cancelled: %s", pct(r.Invoices.Cancelled, r.Invoices.Total))
	line("  with rows: %s", pct(r.Invoices.WithRows, r.Invoices.Total))
	line("  with voucher link: %s", pct(r.Invoices.WithVoucherLink, r.Invoices.Total))
	line("  with cost centre or project on a row: %s", pct(r.Invoices.WithCostCenterOrProject, r.Invoices.Total))
	line("  not in SEK: %s", pct(r.Invoices.NonSEK, r.Invoices.Total))
	line("  dates not in YYYY-MM-DD, left out: %s", cnt(r.Invoices.BadDate))
	line("")
	line("SUPPLIERS: suppliers: %s", cnt(r.Suppliers.Total))
	line("  organisation number: %s", pct(r.Suppliers.WithOrganisationNumber, r.Suppliers.Total))
	line("  VAT number: %s", pct(r.Suppliers.WithVATNumber, r.Suppliers.Total))
	line("  bank account number: %s", pct(r.Suppliers.WithBankAccount, r.Suppliers.Total))
	line("  IBAN: %s", pct(r.Suppliers.WithIBAN, r.Suppliers.Total))
	line("  predefined account: %s", pct(r.Suppliers.WithPredefinedAccount, r.Suppliers.Total))
	line("  invoiced suppliers booked to more than one account: %s", pct(r.Diversity.WithMoreThanOneAccount, r.Diversity.Suppliers))
	line("")
	// usable, trained and held out are one sum: when one or two are held out,
	// printing both of the others exactly would give that count back.
	if held := r.Eval.Usable - r.Eval.Train; held > 0 && held < k {
		line("EVALUATION: usable and trained-on counts withheld, because fewer than %d invoices are held out (no derivable expense account: %s; cancelled: %s; no supplier number: %s)",
			k, cnt(r.Eval.NoAccount), cnt(r.Eval.CancelledSkipped), cnt(r.Eval.NoSupplier))
	} else {
		line("EVALUATION: usable invoices %s (no derivable expense account, such as credit notes and asset purchases: %s; cancelled: %s; no supplier number: %s); trained on %s",
			cnt(r.Eval.Usable), cnt(r.Eval.NoAccount), cnt(r.Eval.CancelledSkipped), cnt(r.Eval.NoSupplier), cnt(r.Eval.Train))
	}
	line("held-out = the latest invoices by date, never a random split.")
	result := func(name string, res booking.Result) {
		line("  %s: tested %s, top-1 %s, top-3 %s, unseen suppliers %s", name, cnt(res.Test), pct(res.Top1, res.Test), pct(res.Top3, res.Test), cnt(res.Unseen))
	}
	result("keyed by the ERP's supplier number", r.ByNumber)
	result("keyed by supplier name (the demo's method)", r.ByName)
	line("  the supplier's predefined account as a baseline: %s of the %s test invoices whose supplier has one", pct(r.Predefined.Right, r.Predefined.Covered), cnt(r.Predefined.Covered))
	line("")
	line("A held-out rate is a measurement on this company's past, not a guarantee about the next invoice.")
	return b.String()
}

// k is the smallest count that may be printed. A count of one or two can be a
// single supplier or invoice.
const k = 3

// cnt prints a count, or "<3" for one or two.
func cnt(n int) string {
	if n > 0 && n < k {
		return fmt.Sprintf("<%d", k)
	}
	return fmt.Sprint(n)
}

// pct prints a rate with its counts. A numerator, denominator or complement of
// one or two is suppressed; zero is printed as it is, because an empty category identifies
// nobody.
func pct(n, d int) string {
	switch {
	case d <= 0:
		return "n/a"
	case d < k:
		return fmt.Sprintf("<%d", k)
	case n > 0 && n < k:
		return fmt.Sprintf("<%d of %d", k, d)
	case d-n > 0 && d-n < k:
		// The complement is one or two: printing n would say so.
		return fmt.Sprintf("all but <%d of %d", k, d)
	}
	return fmt.Sprintf("%.1f%% (%d/%d)", 100*float64(n)/float64(d), n, d)
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
