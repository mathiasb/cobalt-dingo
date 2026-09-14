package domain

import (
	"fmt"
	"sort"
	"strings"
)

// InvoiceStateCount is how many invoices sit under one Fortnox status filter.
type InvoiceStateCount struct {
	Filter string
	Count  int
}

// InvoiceStates is the invoice population by status, for both ledgers.
//
// A slice rather than a map so a report of it is stable between runs: map
// iteration order is random, and two identical measurements that render
// differently look like a change.
type InvoiceStates struct {
	Supplier []InvoiceStateCount
	Customer []InvoiceStateCount
}

// ObligationsVerdict is what the invoice population says about money owed.
type ObligationsVerdict string

const (
	// ObligationsNone means both ledgers are settled, on a measurement
	// complete enough to support that.
	ObligationsNone ObligationsVerdict = "none"

	// ObligationsOpen means invoices are outstanding — the ordinary case.
	ObligationsOpen ObligationsVerdict = "open"

	// ObligationsHidden means nothing is UNPAID but invoices exist that a
	// paid/unpaid view cannot see, because they are unbooked. This is the
	// state in which a report showing SEK 0 payable is actively misleading.
	ObligationsHidden ObligationsVerdict = "hidden"

	// ObligationsUnknown means the measurement cannot support any conclusion.
	// Distinct from "none" for the same reason an absent @TotalResources is
	// not zero: not measuring and measuring nothing are different facts.
	ObligationsUnknown ObligationsVerdict = "unknown"
)

// obligationFilters are the statuses that mean money is still owed or owing.
//
// `unpaid` is the obvious one. `unbooked` is the one that matters: it is a
// separate Fortnox filter, NOT a subset of unpaid, so an unbooked invoice
// appears in neither the paid nor the unpaid view and does not reach the
// general ledger either. `pendingpayment` and `authorizepending` are
// supplier-only and both mean an invoice is in flight — money about to leave.
//
// `unpaidoverdue` is deliberately absent: it is a subset of unpaid, and
// counting both would double-count the same invoices.
var obligationFilters = []string{"unpaid", "unbooked", "pendingpayment", "authorizepending"}

// Assess reports what the invoice population says about outstanding
// obligations, and why.
//
// The reason is returned rather than logged because it is the answer to "the
// books say zero — is that real?", which is a question about evidence.
func (s InvoiceStates) Assess() (ObligationsVerdict, string) {
	if len(s.Supplier) == 0 && len(s.Customer) == 0 {
		return ObligationsUnknown, "invoice states were not measured — refusing to conclude anything about outstanding obligations"
	}

	// The conclusion "nothing outstanding" depends on having looked for
	// unbooked invoices. Without that filter the measurement cannot support
	// it, and a verdict drawn anyway would be confident and wrong.
	for label, ledger := range map[string][]InvoiceStateCount{"supplier": s.Supplier, "customer": s.Customer} {
		if len(ledger) == 0 {
			continue
		}
		if _, ok := find(ledger, "unbooked"); !ok {
			return ObligationsUnknown, fmt.Sprintf(
				"the %s ledger was counted without the `unbooked` filter — an unbooked invoice is an obligation that no paid/unpaid view can see, so this measurement cannot show that nothing is outstanding", label)
		}
	}

	var open, hidden []string
	for _, ledger := range []struct {
		label  string
		counts []InvoiceStateCount
	}{{"supplier", s.Supplier}, {"customer", s.Customer}} {
		for _, f := range obligationFilters {
			n, ok := find(ledger.counts, f)
			if !ok || n == 0 {
				continue
			}
			entry := fmt.Sprintf("%d %s %s", n, ledger.label, f)
			if f == "unbooked" {
				hidden = append(hidden, entry)
				continue
			}
			open = append(open, entry)
		}
	}

	switch {
	case len(open) > 0 && len(hidden) > 0:
		return ObligationsOpen, fmt.Sprintf("%s; also %s, which the ledger balances do not include",
			strings.Join(open, ", "), strings.Join(hidden, ", "))
	case len(open) > 0:
		return ObligationsOpen, strings.Join(open, ", ")
	case len(hidden) > 0:
		return ObligationsHidden, fmt.Sprintf(
			"nothing is unpaid, but %s — an unbooked invoice has not reached the general ledger, so a receivable or payable of zero does NOT mean nothing is owed",
			strings.Join(hidden, ", "))
	}

	return ObligationsNone, fmt.Sprintf("no invoice is unpaid, unbooked or in flight in either ledger (%s)", settledEvidence(s))
}

// settledEvidence names what WAS found, so "nothing outstanding" rests on a
// measurement that clearly ran rather than on an absence of output.
func settledEvidence(s InvoiceStates) string {
	var parts []string
	for _, ledger := range []struct {
		label  string
		counts []InvoiceStateCount
	}{{"supplier", s.Supplier}, {"customer", s.Customer}} {
		for _, f := range []string{"fullypaid", "cancelled"} {
			if n, ok := find(ledger.counts, f); ok && n > 0 {
				parts = append(parts, fmt.Sprintf("%d %s %s", n, ledger.label, f))
			}
		}
	}
	if len(parts) == 0 {
		return "no invoices at all in either ledger"
	}
	sort.Strings(parts)
	return strings.Join(parts, ", ")
}

func find(counts []InvoiceStateCount, filter string) (int, bool) {
	for _, c := range counts {
		if c.Filter == filter {
			return c.Count, true
		}
	}
	return 0, false
}
