package domain

import (
	"strings"
	"testing"
)

func ref(kind UnbookedKind, number int) UnbookedRef {
	return UnbookedRef{Kind: kind, InvoiceNumber: number}
}

// The alert condition Mathias chose: a NEW unbooked invoice appearing. So the
// detection is identity-based — which invoices are unbooked now that were not
// unbooked at the last look.
func TestDetectNewUnbooked_reportsOnlyWhatWasNotSeenBefore(t *testing.T) {
	seen := []UnbookedRef{ref(UnbookedPayable, 144), ref(UnbookedReceivable, 31)}
	current := []UnbookedRef{
		ref(UnbookedPayable, 144),   // already known
		ref(UnbookedReceivable, 31), // already known
		ref(UnbookedPayable, 200),   // new
	}

	change := DetectUnbookedChange(seen, current)

	if len(change.Appeared) != 1 || change.Appeared[0].InvoiceNumber != 200 {
		t.Fatalf("expected only 200 to be new, got %v", change.Appeared)
	}
	if !change.HasAlert() {
		t.Error("a new unbooked invoice must raise an alert")
	}
}

// The steady state. Nothing changed, so nothing is said — that is the whole
// point of alerts-only: a nightly run that reports every night is a nightly
// report, and it gets ignored.
func TestDetectUnbookedChange_nothingNewIsSilent(t *testing.T) {
	seen := []UnbookedRef{ref(UnbookedPayable, 144), ref(UnbookedReceivable, 31)}

	change := DetectUnbookedChange(seen, seen)

	if change.HasAlert() {
		t.Errorf("an unchanged set must be silent, got %+v", change)
	}
	if len(change.Appeared) != 0 {
		t.Errorf("nothing should be new, got %v", change.Appeared)
	}
}

// An invoice that gets booked stops being an obligation. It must leave the
// remembered set, so that if it is ever unbooked AGAIN that is a fresh event
// rather than one suppressed forever by a stale memory.
func TestDetectUnbookedChange_bookedInvoicesAreForgottenSoTheyCanRecur(t *testing.T) {
	seen := []UnbookedRef{ref(UnbookedPayable, 144)}

	change := DetectUnbookedChange(seen, nil)
	if len(change.Resolved) != 1 || change.Resolved[0].InvoiceNumber != 144 {
		t.Fatalf("144 should be reported resolved, got %v", change.Resolved)
	}
	// Resolution alone is good news and does not alert.
	if change.HasAlert() {
		t.Error("an invoice being booked must not raise an alert")
	}

	// Now it comes back. Memory no longer holds it, so it is new again.
	again := DetectUnbookedChange(nil, []UnbookedRef{ref(UnbookedPayable, 144)})
	if len(again.Appeared) != 1 {
		t.Fatalf("a recurrence must alert, got %v", again.Appeared)
	}
}

// Payable #144 and receivable #144 are different invoices. Keying on the
// number alone would suppress a real alert.
func TestDetectUnbookedChange_kindIsPartOfTheIdentity(t *testing.T) {
	seen := []UnbookedRef{ref(UnbookedPayable, 144)}
	current := []UnbookedRef{ref(UnbookedPayable, 144), ref(UnbookedReceivable, 144)}

	change := DetectUnbookedChange(seen, current)

	if len(change.Appeared) != 1 || change.Appeared[0].Kind != UnbookedReceivable {
		t.Fatalf("the receivable must be new, got %v", change.Appeared)
	}
}

// Deterministic order, so an alert body is stable and two identical states
// produce identical text.
func TestDetectUnbookedChange_appearedIsOrdered(t *testing.T) {
	current := []UnbookedRef{
		ref(UnbookedReceivable, 31),
		ref(UnbookedPayable, 200),
		ref(UnbookedPayable, 144),
	}

	change := DetectUnbookedChange(nil, current)

	got := make([]string, 0, len(change.Appeared))
	for _, r := range change.Appeared {
		got = append(got, r.String())
	}
	want := "payable #144, payable #200, receivable #31"
	if strings.Join(got, ", ") != want {
		t.Errorf("got %q, want %q", strings.Join(got, ", "), want)
	}
}

// The same invoice listed twice by the source must not produce two alerts.
func TestDetectUnbookedChange_duplicatesInTheSourceCollapse(t *testing.T) {
	current := []UnbookedRef{ref(UnbookedPayable, 144), ref(UnbookedPayable, 144)}

	change := DetectUnbookedChange(nil, current)

	if len(change.Appeared) != 1 {
		t.Fatalf("expected one alert, got %v", change.Appeared)
	}
}
