// Package receipts implements the IMAP-based receipt collection pipeline.
package receipts

import (
	"context"
	"fmt"
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
	Account        string
	Processed      int
	Routed         int
	UnmatchedCount int
	UnmatchedMails []Mail
	Errors         []error
}

// Collector polls IMAP sources and routes mail via a Router.
type Collector struct {
	sources []*Source
	router  *Router
	dryRun  bool
	smtp    SMTPConfig

	// Seams — overridable in tests. Default to the real IMAP/SMTP impls.
	fetch     func(src *Source) ([]Mail, error)
	deliverFn func(m Mail, dest *Destination) error
	markSeen  func(src *Source, uids []uint32) error
}

// NewCollector constructs a Collector. When dryRun is true no mail is
// forwarded and nothing is marked \Seen. Mail is always fetched with PEEK;
// a message is only marked \Seen after it forwards successfully.
func NewCollector(sources []*Source, router *Router, dryRun bool, smtp SMTPConfig) *Collector {
	c := &Collector{sources: sources, router: router, dryRun: dryRun, smtp: smtp}
	c.fetch = fetchMails
	c.deliverFn = c.deliver
	c.markSeen = markSeen
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

	mails, err := c.fetch(src)
	if err != nil {
		return result, err
	}

	// UIDs of mails that forwarded successfully — only these get marked \Seen,
	// so an unmatched or failed-to-forward mail is never silently consumed.
	var forwarded []uint32

	for _, m := range mails {
		result.Processed++
		dest, ok := c.router.Route(m)
		if !ok {
			result.UnmatchedCount++
			result.UnmatchedMails = append(result.UnmatchedMails, m)
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
	}

	if !c.dryRun && len(forwarded) > 0 {
		if err := c.markSeen(src, forwarded); err != nil {
			result.Errors = append(result.Errors, fmt.Errorf("markera \\Seen: %w", err))
		}
	}
	return result, nil
}
