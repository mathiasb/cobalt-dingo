// Package receipts implements the IMAP-based receipt collection pipeline.
package receipts

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Mail represents a fetched email message.
type Mail struct {
	UID         uint32
	MessageID   string
	From        string
	Subject     string
	Date        time.Time
	Body        string
	Attachments []Attachment
}

// Attachment is a single email attachment.
type Attachment struct {
	Filename    string
	ContentType string
	Data        []byte
}

// Source describes an IMAP account to poll.
type Source struct {
	Name     string
	Host     string
	Port     int
	Username string
	Password string
	Folder   string
	TLS      bool
}

// CollectorResult summarises one account's collection run.
type CollectorResult struct {
	Account   string
	Processed int
	Routed    int

	// RoutedMails records what WOULD be, or was, forwarded and where. A dry run
	// that reports only a count cannot be reviewed: the operator has to see the
	// sender, the subject and the destination before mail moves.
	RoutedMails    []RoutedMail
	UnmatchedCount int
	UnmatchedMails []Mail

	// DuplicateCount and DuplicateMails record mail that matched a rule but was
	// already forwarded to the same destination by hand. Reported rather than
	// dropped silently: a growing duplicate count is how a stale hand-forwarding
	// habit becomes visible.
	DuplicateCount int
	DuplicateMails []Mail

	// ForwardedSubjects is how many DISTINCT normalised subjects the duplicate
	// guard found in Sent. It is not a denominator for DuplicateCount: thirty
	// Apple invoices share one subject, so the counts measure different things.
	ForwardedSubjects int

	// MissedCount and MissedMails are the honest coverage gap — mail Mathias
	// forwarded by hand that NO rule matched. Each one is a receipt this tool
	// would have left behind, and a rule waiting to be written.
	MissedCount int
	MissedMails []Mail

	// SelfSentSkipped counts mail the account sent itself. All Mail includes
	// Sent, so every hand-forward comes back as a candidate with the account as
	// sender. Routing one would forward a forward; counting one as a miss made
	// the coverage gap look twenty times larger than it is.
	SelfSentSkipped int

	Errors []error
}

// RoutedMail is one routing decision, for review.
type RoutedMail struct {
	Mail        Mail
	Destination string
}

// Collector polls IMAP sources and routes mail via a Router.
type Collector struct {
	sources []*Source
	router  *Router
	dryRun  bool
	scope   *Scope
	smtp    SMTPConfig

	// Seams — overridable in tests. Default to the real IMAP/SMTP impls.
	fetch         func(src *Source, scope *Scope, collected CollectedSet) ([]Mail, error)
	deliverFn     func(m Mail, dest *Destination) error
	markSeen      func(src *Source, scope *Scope, uids []uint32) error
	loadForwarded func(src *Source, scope *Scope) (ForwardedIndex, error)
}

// NewCollector constructs a Collector. When dryRun is true no mail is
// forwarded and nothing is marked \Seen. Mail is always fetched with PEEK;
// a message is only marked \Seen after it forwards successfully.
func NewCollector(sources []*Source, router *Router, dryRun bool, smtp SMTPConfig) *Collector {
	c := &Collector{sources: sources, router: router, dryRun: dryRun, smtp: smtp}
	c.fetch = fetchMails
	c.deliverFn = c.deliver
	c.markSeen = markCollected
	c.loadForwarded = c.loadForwardedByHand
	return c
}

// Run processes every source account and returns one result per account.
func (c *Collector) Run(ctx context.Context) ([]CollectorResult, error) {
	results := make([]CollectorResult, 0, len(c.sources))
	for _, src := range c.sources {
		result, err := c.processAccount(ctx, src)
		if err != nil {
			return results, fmt.Errorf("konto %s: %w", src.Name, err)
		}
		results = append(results, result)
	}
	return results, nil
}

func (c *Collector) processAccount(_ context.Context, src *Source) (CollectorResult, error) {
	result := CollectorResult{Account: src.Name}

	if c.scope == nil {
		// Fail closed. An unbounded collector against a real backlog searches
		// tens of thousands of messages (#79); panicking on a nil scope, or
		// worse silently defaulting to everything, is not an option.
		return result, fmt.Errorf("collector has no scope: call WithScope before Run")
	}

	collected, err := loadCollected(src, c.scope)
	if err != nil {
		return result, err
	}
	if c.loadForwarded == nil {
		// Fail closed rather than dereferencing nil. A missing duplicate guard is
		// the exact defect this guard exists to prevent: the collector and
		// Mathias reach the same inboxes.
		return result, fmt.Errorf("collector has no duplicate guard: refusing to run, since it would re-forward every receipt already sent by hand")
	}

	// Fail closed. An unreadable index is indistinguishable from an empty one,
	// and an empty one means every receipt forwarded by hand goes again.
	forwardedByHand, err := c.loadForwarded(src, c.scope)
	if err != nil {
		return result, err
	}
	result.ForwardedSubjects = len(forwardedByHand)

	mails, err := c.fetch(src, c.scope, collected)
	if err != nil {
		return result, err
	}

	// UIDs of mails that forwarded successfully — only these get marked \Seen,
	// so an unmatched or failed-to-forward mail is never silently consumed.
	var forwarded []uint32

	for _, m := range mails {
		result.Processed++
		if strings.EqualFold(strings.TrimSpace(m.From), strings.TrimSpace(src.Username)) {
			result.SelfSentSkipped++
			continue
		}
		dest, ok := c.router.Route(m)
		if !ok {
			result.UnmatchedCount++
			result.UnmatchedMails = append(result.UnmatchedMails, m)
			// He forwarded this one himself and no rule caught it. That is the
			// coverage gap, measurable only because the guard knows what he sent.
			if forwardedByHand.Has(m.Subject) {
				result.MissedCount++
				result.MissedMails = append(result.MissedMails, m)
			}
			continue
		}
		if forwardedByHand.Has(m.Subject) {
			result.DuplicateCount++
			result.DuplicateMails = append(result.DuplicateMails, m)
			continue
		}
		if !c.dryRun {
			if err := c.deliverFn(m, dest); err != nil {
				result.Errors = append(result.Errors, fmt.Errorf("vidarebefordra %s: %w", m.MessageID, err))
				continue
			}
			forwarded = append(forwarded, m.UID)
		}
		result.Routed++
		result.RoutedMails = append(result.RoutedMails, RoutedMail{Mail: m, Destination: dest.Name})
	}

	if !c.dryRun && len(forwarded) > 0 {
		if err := c.markSeen(src, c.scope, forwarded); err != nil {
			result.Errors = append(result.Errors, fmt.Errorf("markera \\Seen: %w", err))
		}
	}
	return result, nil
}

// WithScope bounds what a run may process. Required before Run: an unbounded
// collector against a real backlog searches tens of thousands of messages and
// fetches every body (#79).
func (c *Collector) WithScope(s *Scope) *Collector {
	c.scope = s
	return c
}
