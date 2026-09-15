package domain

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type fakeUnbookedStore struct {
	seen        []UnbookedRef
	seenErr     error
	reconciled  *UnbookedChange
	reconcErr   error
	reconcCalls int
}

func (f *fakeUnbookedStore) Seen(context.Context, TenantID) ([]UnbookedRef, error) {
	return f.seen, f.seenErr
}

func (f *fakeUnbookedStore) Reconcile(_ context.Context, _ TenantID, c UnbookedChange) error {
	f.reconcCalls++
	f.reconciled = &c
	return f.reconcErr
}

type fakeAlerter struct {
	subject, body string
	calls         int
	err           error
}

func (f *fakeAlerter) Alert(_ context.Context, subject, body string) error {
	f.calls++
	f.subject, f.body = subject, body
	return f.err
}

func current() []UnbookedRef {
	return []UnbookedRef{{Kind: UnbookedPayable, InvoiceNumber: 200}}
}

func TestUnbookedWatch_alertsOnANewInvoiceThenRemembersIt(t *testing.T) {
	store := &fakeUnbookedStore{}
	alerter := &fakeAlerter{}

	change, err := NewUnbookedWatch(store, alerter).Check(context.Background(), "t1", current())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if alerter.calls != 1 {
		t.Fatalf("expected one alert, got %d", alerter.calls)
	}
	if !strings.Contains(alerter.body, "payable #200") {
		t.Errorf("alert body must name the invoice, got: %s", alerter.body)
	}
	if store.reconcCalls != 1 || len(store.reconciled.Appeared) != 1 {
		t.Errorf("the appearance must be remembered after alerting, got %+v", store.reconciled)
	}
	if !change.HasAlert() {
		t.Error("the caller needs to know an alert was raised")
	}
}

// The ordering that matters. If the alert fails, the appearance must NOT be
// remembered: remembering it would mark an obligation as already reported that
// nobody was ever told about, and the next run would stay silent forever.
// Better to retry and risk a duplicate than to go quiet.
func TestUnbookedWatch_aFailedAlertIsNotRemembered(t *testing.T) {
	store := &fakeUnbookedStore{}
	alerter := &fakeAlerter{err: errors.New("gitea unreachable")}

	_, err := NewUnbookedWatch(store, alerter).Check(context.Background(), "t1", current())

	if err == nil {
		t.Fatal("a failed alert must be an error — silence is the failure mode here")
	}
	if store.reconcCalls != 0 {
		t.Error("nothing may be remembered when the alert did not get through")
	}
}

// Steady state: nothing new, so no alert. Resolutions are still persisted,
// because an invoice left in memory could never be reported again if it
// recurred.
func TestUnbookedWatch_silentButStillForgetsResolved(t *testing.T) {
	store := &fakeUnbookedStore{seen: []UnbookedRef{
		{Kind: UnbookedPayable, InvoiceNumber: 144},
	}}
	alerter := &fakeAlerter{}

	change, err := NewUnbookedWatch(store, alerter).Check(context.Background(), "t1", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if alerter.calls != 0 {
		t.Error("a resolution must not alert")
	}
	if store.reconcCalls != 1 || len(store.reconciled.Resolved) != 1 {
		t.Errorf("the resolution must be persisted, got %+v", store.reconciled)
	}
	if change.HasAlert() {
		t.Error("nothing to alert about")
	}
}

// An unreadable memory must not be treated as an empty one: every currently
// unbooked invoice would look new and alert, training the reader to ignore it.
func TestUnbookedWatch_unreadableMemoryIsAnErrorNotAnEmptySet(t *testing.T) {
	store := &fakeUnbookedStore{seenErr: errors.New("db down")}
	alerter := &fakeAlerter{}

	_, err := NewUnbookedWatch(store, alerter).Check(context.Background(), "t1", current())

	if err == nil {
		t.Fatal("expected an error")
	}
	if alerter.calls != 0 {
		t.Error("must not alert on a set it could not compare against")
	}
}

// The alert was already delivered, so a reconcile failure cannot be undone.
// It must surface — the consequence is a duplicate alert next run, which is
// the acceptable direction.
func TestUnbookedWatch_reconcileFailureAfterAlertingIsSurfaced(t *testing.T) {
	store := &fakeUnbookedStore{reconcErr: errors.New("db down")}
	alerter := &fakeAlerter{}

	_, err := NewUnbookedWatch(store, alerter).Check(context.Background(), "t1", current())

	if err == nil {
		t.Fatal("a failed reconcile must be surfaced")
	}
	if alerter.calls != 1 {
		t.Error("the alert had already been sent")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "already sent") {
		t.Errorf("the error must say the alert went out, so a duplicate is expected: %v", err)
	}
}
