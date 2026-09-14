// Package report renders a domain.FinancialOverview as plain text.
//
// Separate from the domain so the arithmetic has no opinion about presentation
// — and so the one presentation rule that is not cosmetic can be tested:
// ADR-0006's kill condition is a report that shows a cached figure without its
// as-of date, which makes the as-of line a correctness property, not layout.
package report

import (
	"fmt"
	"strings"

	"github.com/mathiasb/cobalt-dingo/internal/domain"
)

// Render writes the overview as plain text, suitable for a Job log.
func Render(ov domain.FinancialOverview) string {
	var b strings.Builder

	fmt.Fprintf(&b, "FINANCIAL POSITION — financial year %d\n", ov.Year)
	fmt.Fprintf(&b, "As of %s (%d vouchers read)\n", ov.AsOf.Format("2006-01-02 15:04 MST"), ov.VoucherCount)

	// The provenance line comes before any figure, in every state. A reader
	// who stops after the header has still seen how much to trust what follows.
	switch ov.Freshness {
	case domain.CacheFresh:
		b.WriteString("Source: cache, verified complete against Fortnox's own count\n")
	case domain.CacheUnverified:
		fmt.Fprintf(&b, "!! UNVERIFIED — completeness could not be checked: %s\n", ov.Reason)
		fmt.Fprintf(&b, "!! Figures below reflect what was cached at %s and may be incomplete.\n",
			ov.AsOf.Format("2006-01-02 15:04 MST"))
	case domain.CacheStale:
		fmt.Fprintf(&b, "!! UNVERIFIED — set was stale and not refreshed: %s\n", ov.Reason)
	default:
		fmt.Fprintf(&b, "!! UNVERIFIED — unknown freshness %q\n", ov.Freshness)
	}

	b.WriteString("\nINTEGRITY\n")
	if ov.Balances() {
		b.WriteString("  Balances: yes — every account's closing balance sums to zero\n")
	} else {
		fmt.Fprintf(&b, "  !! DOES NOT BALANCE — out by %s\n", ov.Discrepancy.String())
		b.WriteString("  !! Brought-forward figures or the voucher set are incomplete.\n")
		b.WriteString("  !! Treat every total below as unreliable.\n")
	}
	if len(ov.UnbalancedVouchers) > 0 {
		fmt.Fprintf(&b, "  !! %d voucher(s) do not balance internally:\n", len(ov.UnbalancedVouchers))
		for _, u := range ov.UnbalancedVouchers {
			fmt.Fprintf(&b, "     %s/%d out by %s — %s\n", u.Series, u.Number, u.Difference.String(), u.Description)
			// The rows as read. Fortnox will not accept an unbalanced voucher
			// through its own UI, so the difference means either the API
			// returned an incomplete set or this code dropped some — and the
			// only way to tell is to see them.
			if len(u.Rows) == 0 {
				b.WriteString("        (no rows returned at all)\n")
			}
			for _, r := range u.Rows {
				fmt.Fprintf(&b, "        %-6d debit %16s  credit %16s  %s\n",
					r.Account, r.Debit.String(), r.Credit.String(), r.Description)
			}
		}
	}

	b.WriteString("\nPOSITION\n")
	fmt.Fprintf(&b, "  Assets                     %20s\n", ov.Assets.String())
	fmt.Fprintf(&b, "  Equity and liabilities     %20s\n", ov.EquityAndLiabilities.String())
	fmt.Fprintf(&b, "  Revenue                    %20s\n", ov.Revenue.String())
	fmt.Fprintf(&b, "  Costs                      %20s\n", ov.Costs.String())
	fmt.Fprintf(&b, "  Result                     %20s\n", ov.Result.String())

	b.WriteString("\nWORKING CAPITAL\n")
	// The verdict comes FIRST, before the figures it qualifies. A reader who
	// sees "Receivables SEK 0.00" and stops has been told the company is owed
	// nothing, which is only true if unbooked invoices were counted and found
	// to be zero (#91).
	verdict, why := ov.Obligations.Assess()
	switch verdict {
	case domain.ObligationsNone:
		fmt.Fprintf(&b, "  Outstanding: none — %s\n", why)
	case domain.ObligationsOpen:
		fmt.Fprintf(&b, "  Outstanding: %s\n", why)
	case domain.ObligationsHidden:
		fmt.Fprintf(&b, "  !! Outstanding: %s\n", why)
	case domain.ObligationsUnknown:
		fmt.Fprintf(&b, "  !! Outstanding: UNKNOWN — %s\n", why)
	default:
		fmt.Fprintf(&b, "  !! Outstanding: unrecognised verdict %q\n", verdict)
	}
	fmt.Fprintf(&b, "  Receivables                %20s\n", ov.Receivables.String())
	fmt.Fprintf(&b, "    not yet due              %20s\n", ov.ReceivablesNotYetDue.String())
	fmt.Fprintf(&b, "    overdue 1-30 d           %20s\n", ov.ReceivablesOverdue0to30.String())
	fmt.Fprintf(&b, "    overdue 31-90 d          %20s\n", ov.ReceivablesOverdue31to90.String())
	fmt.Fprintf(&b, "    overdue 90+ d            %20s\n", ov.ReceivablesOverdue90Plus.String())
	fmt.Fprintf(&b, "  Payables                   %20s\n", ov.Payables.String())
	fmt.Fprintf(&b, "    not yet due              %20s\n", ov.PayablesNotYetDue.String())
	fmt.Fprintf(&b, "    overdue 1-30 d           %20s\n", ov.PayablesOverdue0to30.String())
	fmt.Fprintf(&b, "    overdue 31-90 d          %20s\n", ov.PayablesOverdue31to90.String())
	fmt.Fprintf(&b, "    overdue 90+ d            %20s\n", ov.PayablesOverdue90Plus.String())

	if len(ov.Obligations.Supplier) > 0 || len(ov.Obligations.Customer) > 0 {
		b.WriteString("\nINVOICES BY STATUS (counts, from Fortnox's own totals)\n")
		renderStates(&b, "supplier", ov.Obligations.Supplier)
		renderStates(&b, "customer", ov.Obligations.Customer)
	}

	// One account per line, because the first thing anyone does with a Job log
	// is grep it for an account number.
	b.WriteString("\nACCOUNTS (opening + movement = closing)\n")
	for _, p := range ov.Positions {
		fmt.Fprintf(&b, "  %-6d %-30s %18s %18s %18s %18s\n",
			p.Number, truncate(p.Description, 30),
			p.Opening.String(), p.Debit.String(), p.Credit.String(), p.Closing.String())
	}

	return b.String()
}

// renderStates prints one filter per line, in the order measured, so two
// identical measurements render identically.
func renderStates(b *strings.Builder, label string, counts []domain.InvoiceStateCount) {
	if len(counts) == 0 {
		fmt.Fprintf(b, "  %-9s (not measured)\n", label)
		return
	}
	for _, c := range counts {
		fmt.Fprintf(b, "  %-9s %-18s %6d\n", label, c.Filter, c.Count)
	}
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}
