package domain

import (
	"strings"
	"testing"
)

func counts(pairs ...any) []InvoiceStateCount {
	out := make([]InvoiceStateCount, 0, len(pairs)/2)
	for i := 0; i < len(pairs); i += 2 {
		out = append(out, InvoiceStateCount{Filter: pairs[i].(string), Count: pairs[i+1].(int)})
	}
	return out
}

// The question #91 asks: the production books report SEK 0 receivable and SEK
// 0 payable, while Mathias expects several open invoices. A zero under
// `unpaid` alone cannot tell a settled company from one whose invoices are all
// unbooked, because `unbooked` is a separate Fortnox filter and not a subset
// of `unpaid`.
func TestInvoiceStates_allZeroMeansNothingOutstanding(t *testing.T) {
	s := InvoiceStates{
		Supplier: counts("unpaid", 0, "unbooked", 0, "pendingpayment", 0, "authorizepending", 0, "fullypaid", 140),
		Customer: counts("unpaid", 0, "unbooked", 0, "fullypaid", 29),
	}

	kind, why := s.Assess()
	if kind != ObligationsNone {
		t.Fatalf("got %q, want none — %s", kind, why)
	}
	if !strings.Contains(why, "140") || !strings.Contains(why, "29") {
		t.Errorf("the reason must cite the settled counts as evidence, got: %s", why)
	}
}

// The case that would make the report misleading rather than merely
// surprising: obligations that exist but are invisible to a paid/unpaid view.
func TestInvoiceStates_unbookedInvoicesAreObligationsTheReportWouldMiss(t *testing.T) {
	s := InvoiceStates{
		Supplier: counts("unpaid", 0, "unbooked", 7, "fullypaid", 140),
		Customer: counts("unpaid", 0, "unbooked", 0, "fullypaid", 29),
	}

	kind, why := s.Assess()
	if kind != ObligationsHidden {
		t.Fatalf("got %q, want hidden — %s", kind, why)
	}
	if !strings.Contains(why, "unbooked") || !strings.Contains(strings.ToLower(why), "7") {
		t.Errorf("the reason must name the unbooked count, got: %s", why)
	}
}

func TestInvoiceStates_openUnpaidInvoicesAreOrdinary(t *testing.T) {
	s := InvoiceStates{
		Supplier: counts("unpaid", 3, "unbooked", 0),
		Customer: counts("unpaid", 0, "unbooked", 0),
	}

	kind, why := s.Assess()
	if kind != ObligationsOpen {
		t.Fatalf("got %q, want open — %s", kind, why)
	}
	if !strings.Contains(why, "3") {
		t.Errorf("reason must cite the count, got: %s", why)
	}
}

// pendingpayment and authorizepending are supplier-only states, and both mean
// an invoice is in flight. Treating them as settled would hide money that is
// about to leave.
func TestInvoiceStates_inFlightSupplierInvoicesCountAsOpen(t *testing.T) {
	for _, filter := range []string{"pendingpayment", "authorizepending"} {
		t.Run(filter, func(t *testing.T) {
			s := InvoiceStates{Supplier: counts("unpaid", 0, "unbooked", 0, filter, 2)}
			kind, why := s.Assess()
			if kind != ObligationsOpen {
				t.Fatalf("got %q for %s, want open — %s", kind, filter, why)
			}
			if !strings.Contains(why, filter) {
				t.Errorf("reason must name %s, got: %s", filter, why)
			}
		})
	}
}

// Cancelled and fullypaid are settled. Counting them as obligations would make
// every company with history look like it owes money.
func TestInvoiceStates_settledStatesAreNotObligations(t *testing.T) {
	s := InvoiceStates{
		Supplier: counts("unpaid", 0, "unbooked", 0, "fullypaid", 140, "cancelled", 4),
		Customer: counts("unpaid", 0, "unbooked", 0, "fullypaid", 29, "cancelled", 1),
	}
	if kind, why := s.Assess(); kind != ObligationsNone {
		t.Fatalf("got %q, want none — %s", kind, why)
	}
}

// An empty measurement is not a finding. Assess must refuse to conclude
// "nothing outstanding" from counts it never took — the same distinction as
// an absent @TotalResources decoding to zero.
func TestInvoiceStates_noCountsAtAllIsUnknownNotNone(t *testing.T) {
	kind, why := InvoiceStates{}.Assess()
	if kind != ObligationsUnknown {
		t.Fatalf("got %q, want unknown — %s", kind, why)
	}
	if !strings.Contains(strings.ToLower(why), "not measured") {
		t.Errorf("reason must say the states were not measured, got: %s", why)
	}
}

// A filter list missing `unbooked` cannot support the conclusion this type
// exists to draw, so it must not be allowed to report "nothing outstanding".
func TestInvoiceStates_missingUnbookedFilterIsUnknown(t *testing.T) {
	s := InvoiceStates{
		Supplier: counts("unpaid", 0, "fullypaid", 140),
		Customer: counts("unpaid", 0, "unbooked", 0),
	}
	kind, why := s.Assess()
	if kind != ObligationsUnknown {
		t.Fatalf("got %q, want unknown — %s", kind, why)
	}
	if !strings.Contains(why, "unbooked") {
		t.Errorf("reason must name the missing filter, got: %s", why)
	}
}
