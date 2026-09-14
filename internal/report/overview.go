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

// names controls whether counterparty names appear in the output.
//
// Fail-closed by construction: namesRedacted is the ZERO value, so Render —
// the function every caller reaches for — redacts, and showing names requires
// naming the decision. Operator discipline is not a control, and a redaction
// pass someone has to remember to apply is operator discipline with extra
// steps (brain: structural-opt-in-marker-fail-closed).
//
// Why it matters here: this output lands in a Job log, agent sessions read Job
// logs, and claudewatcher ingests those transcripts into the brain wiki —
// which the homelab client list must never reach (#94). One of the two
// unbooked invoices found on 2026-09-14 named a company on that list.
type names int

const (
	namesRedacted names = iota // zero value
	namesShown
)

// redactedName is what replaces a withheld name. Not an empty string: a blank
// where a name should be reads as missing data, and someone will go looking
// for the bug.
const redactedName = "[name withheld]"

// Render writes the overview as plain text with counterparty names REDACTED,
// suitable for a Job log or a transcript.
//
// Invoice numbers, amounts and dates survive redaction, so the output stays
// actionable — a document number is enough to find anything in Fortnox.
func Render(ov domain.FinancialOverview) string {
	return render(ov, namesRedacted)
}

// RenderWithNames includes counterparty names. For a destination that is known
// not to flow into the brain wiki or a cloud API.
func RenderWithNames(ov domain.FinancialOverview) string {
	return render(ov, namesShown)
}

func render(ov domain.FinancialOverview, show names) string {
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
			fmt.Fprintf(&b, "     %s/%d out by %s — %s\n", u.Series, u.Number, u.Difference.String(), reveal(u.Description, show))
			// The rows as read. Fortnox will not accept an unbalanced voucher
			// through its own UI, so the difference means either the API
			// returned an incomplete set or this code dropped some — and the
			// only way to tell is to see them.
			if len(u.Rows) == 0 {
				b.WriteString("        (no rows returned at all)\n")
			}
			for _, r := range u.Rows {
				fmt.Fprintf(&b, "        %-6d debit %16s  credit %16s  %s\n",
					r.Account, r.Debit.String(), r.Credit.String(), reveal(r.Description, show))
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

	if len(ov.UnbookedSupplier) > 0 || len(ov.UnbookedCustomer) > 0 {
		b.WriteString("\nUNBOOKED INVOICES (not in any ledger balance above)\n")
		for _, inv := range ov.UnbookedSupplier {
			fmt.Fprintf(&b, "  payable  #%-8d %-28s %16s  due %s  ref %s\n",
				inv.InvoiceNumber, truncate(reveal(inv.SupplierName, show), 28), inv.Balance.String(), inv.DueDate, inv.SupplierReference)
		}
		for _, inv := range ov.UnbookedCustomer {
			fmt.Fprintf(&b, "  receivable #%-6d %-28s %16s  due %s\n",
				inv.InvoiceNumber, truncate(reveal(inv.CustomerName, show), 28), inv.Balance.String(), inv.DueDate)
		}
	}

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

// reveal returns a counterparty-bearing string only when names are shown.
//
// Deliberately applied at every use site rather than by scrubbing the finished
// output: a scrub is a denylist over free text, and this estate has already
// proved that filtering output is not a control — an English keyword filter
// missed a Swedish mail subject (email-triage-agent #16). Choosing per field
// what is a name cannot silently miss one.
func reveal(value string, show names) string {
	if show == namesShown {
		return value
	}
	if strings.TrimSpace(value) == "" {
		return ""
	}
	return redactedName
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}
