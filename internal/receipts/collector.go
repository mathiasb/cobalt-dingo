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
}

// NewCollector constructs a Collector. When dryRun is true no mail is
// actually forwarded.
func NewCollector(sources []*Source, router *Router, dryRun bool) *Collector {
	return &Collector{sources: sources, router: router, dryRun: dryRun}
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

func (c *Collector) processAccount(ctx context.Context, src *Source) (CollectorResult, error) {
	result := CollectorResult{Account: src.Name}

	mails, err := fetchMails(ctx, src)
	if err != nil {
		return result, err
	}

	for _, m := range mails {
		result.Processed++
		dest, ok := c.router.Route(m)
		if !ok {
			result.UnmatchedCount++
			result.UnmatchedMails = append(result.UnmatchedMails, m)
			continue
		}
		if !c.dryRun {
			if err := deliver(ctx, m, dest); err != nil {
				result.Errors = append(result.Errors, fmt.Errorf("vidarebefordra %s: %w", m.MessageID, err))
				continue
			}
		}
		result.Routed++
	}
	return result, nil
}

// fetchMails connects to an IMAP server and returns unseen messages.
// Implemented via github.com/emersion/go-imap/v2.
func fetchMails(_ context.Context, _ *Source) ([]Mail, error) {
	// TODO: dial TLS, select folder, search UNSEEN, fetch envelope+body
	return nil, nil
}

// deliver forwards a mail to its destination (SMTP relay or Fortnox inbox).
func deliver(_ context.Context, _ Mail, _ *Destination) error {
	// TODO: smtp.SendMail for type "smtp"; Fortnox /inbox upload for fortnox-*
	return nil
}
