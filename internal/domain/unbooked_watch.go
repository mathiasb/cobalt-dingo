package domain

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// UnbookedKind distinguishes an obligation the company owes from one owed to
// it. Part of an invoice's identity here: payable #144 and receivable #144 are
// different invoices, and keying on the number alone would suppress a real
// alert.
type UnbookedKind string

const (
	UnbookedPayable    UnbookedKind = "payable"
	UnbookedReceivable UnbookedKind = "receivable"
)

// UnbookedRef identifies one unbooked invoice.
//
// Identity only — no amount or due date. The watched condition is "a new
// unbooked invoice appeared", so comparing amounts would raise an alert when
// an existing invoice is edited, which is a different event and not the one
// being watched. Recorded as a known limitation rather than a silent choice.
type UnbookedRef struct {
	Kind          UnbookedKind
	InvoiceNumber int
}

// String renders the reference for an alert body.
func (r UnbookedRef) String() string { return fmt.Sprintf("%s #%d", r.Kind, r.InvoiceNumber) }

// UnbookedChange is the difference between what was remembered and what is
// unbooked now.
type UnbookedChange struct {
	// Appeared is unbooked now and was not before. This is the alert.
	Appeared []UnbookedRef

	// Resolved was unbooked and is not any more — it was booked or cancelled.
	// Good news, so it does not alert, but it must be persisted: an invoice
	// left in memory forever could never be reported again if it recurred.
	Resolved []UnbookedRef
}

// HasAlert reports whether anything happened that is worth waking someone for.
//
// Resolution deliberately does not count. A nightly job that speaks whenever
// anything changes becomes a nightly report, and a nightly report gets
// ignored — which is the failure mode alerts-only exists to avoid.
func (c UnbookedChange) HasAlert() bool { return len(c.Appeared) > 0 }

// DetectUnbookedChange compares the remembered set with the current one.
//
// Pure, so every rule above is testable without a database or an API.
func DetectUnbookedChange(seen, current []UnbookedRef) UnbookedChange {
	was := index(seen)
	is := index(current)

	var change UnbookedChange
	for ref := range is {
		if _, known := was[ref]; !known {
			change.Appeared = append(change.Appeared, ref)
		}
	}
	for ref := range was {
		if _, still := is[ref]; !still {
			change.Resolved = append(change.Resolved, ref)
		}
	}

	// Deterministic order, so an alert body is stable and two identical states
	// produce identical text rather than looking like a change.
	sortRefs(change.Appeared)
	sortRefs(change.Resolved)
	return change
}

// index also collapses duplicates: a source that lists the same invoice twice
// must not produce two alerts.
func index(refs []UnbookedRef) map[UnbookedRef]struct{} {
	m := make(map[UnbookedRef]struct{}, len(refs))
	for _, r := range refs {
		m[r] = struct{}{}
	}
	return m
}

func sortRefs(refs []UnbookedRef) {
	sort.Slice(refs, func(i, j int) bool {
		if refs[i].Kind != refs[j].Kind {
			return refs[i].Kind < refs[j].Kind
		}
		return refs[i].InvoiceNumber < refs[j].InvoiceNumber
	})
}

// UnbookedStore remembers which invoices were unbooked at the last look.
//
// Implementations: internal/adapter/postgres
type UnbookedStore interface {
	// Seen returns the remembered set for a tenant.
	Seen(ctx context.Context, tenantID TenantID) ([]UnbookedRef, error)

	// Reconcile records newly appeared references and forgets resolved ones,
	// so the store always mirrors "currently unbooked".
	Reconcile(ctx context.Context, tenantID TenantID, change UnbookedChange) error
}

// Alerter delivers an alert. Separate from detection so the channel can change
// without touching the rule, and so a channel outage cannot silently turn
// detection off.
type Alerter interface {
	Alert(ctx context.Context, subject, body string) error
}

// UnbookedWatch reports newly appeared unbooked invoices, once each.
type UnbookedWatch struct {
	store   UnbookedStore
	alerter Alerter
}

// NewUnbookedWatch constructs the watch.
func NewUnbookedWatch(store UnbookedStore, alerter Alerter) *UnbookedWatch {
	return &UnbookedWatch{store: store, alerter: alerter}
}

// Check compares the current unbooked set with what was remembered, alerts on
// anything new, and then updates the memory.
//
// The ORDER is the design. Alert first, remember second: remembering first
// would mark an obligation as already reported when the alert had not gone
// out, and every later run would then stay silent about it. A duplicate alert
// is recoverable; a permanently suppressed one is not.
func (w *UnbookedWatch) Check(ctx context.Context, tenantID TenantID, currentSet []UnbookedRef) (UnbookedChange, error) {
	seen, err := w.store.Seen(ctx, tenantID)
	if err != nil {
		// NOT treated as an empty set. Every currently unbooked invoice would
		// look new, alert, and train the reader to ignore the channel.
		return UnbookedChange{}, fmt.Errorf("read remembered unbooked invoices: %w", err)
	}

	change := DetectUnbookedChange(seen, currentSet)

	if change.HasAlert() {
		subject, body := describeUnbookedAlert(change)
		if err := w.alerter.Alert(ctx, subject, body); err != nil {
			// Deliberately no reconcile. See the ordering note above.
			return change, fmt.Errorf("deliver unbooked-invoice alert: %w", err)
		}
	}

	if err := w.store.Reconcile(ctx, tenantID, change); err != nil {
		return change, fmt.Errorf("record unbooked state (alert ALREADY sent, so expect a duplicate next run): %w", err)
	}
	return change, nil
}

// describeUnbookedAlert builds the alert text.
//
// Names no counterparty and carries no amount — the alert travels to a channel
// whose reach is not controlled here, and an invoice number is enough to find
// the thing in Fortnox (#94).
func describeUnbookedAlert(change UnbookedChange) (subject, body string) {
	refs := make([]string, 0, len(change.Appeared))
	for _, r := range change.Appeared {
		refs = append(refs, r.String())
	}
	joined := strings.Join(refs, ", ")

	subject = fmt.Sprintf("%d new unbooked invoice(s) in Fortnox", len(change.Appeared))
	body = fmt.Sprintf(
		"These invoices are registered in Fortnox but not booked, so they appear in "+
			"neither the unpaid view nor the general ledger — every balance the overview "+
			"reports excludes them:\n\n  %s\n\n"+
			"Look them up by number in Fortnox. Counterparties and amounts are omitted "+
			"here on purpose: this alert's destination is not a controlled one.\n",
		joined)
	return subject, body
}
