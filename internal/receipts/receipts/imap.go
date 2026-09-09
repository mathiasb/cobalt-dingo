package receipts

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"io"
	"strings"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/emersion/go-message"
	_ "github.com/emersion/go-message/charset" // register charsets for decoding
	"github.com/emersion/go-message/mail"
)

// dialAndSelect connects, logs in, and selects the source's folder.
func dialAndSelect(src *Source, folder string) (*imapclient.Client, error) {
	if folder == "" {
		folder = src.Folder
	}
	if folder == "" {
		folder = "INBOX"
	}

	addr := fmt.Sprintf("%s:%d", src.Host, src.Port)
	var (
		c   *imapclient.Client
		err error
	)
	if src.TLS {
		c, err = imapclient.DialTLS(addr, &imapclient.Options{
			TLSConfig: &tls.Config{ServerName: src.Host},
		})
	} else {
		c, err = imapclient.DialStartTLS(addr, nil)
	}
	if err != nil {
		return nil, fmt.Errorf("imap dial %s: %w", addr, err)
	}

	if err := c.Login(src.Username, src.Password).Wait(); err != nil {
		_ = c.Close()
		return nil, fmt.Errorf("imap login %s: %w", src.Username, err)
	}
	if _, err := c.Select(folder, nil).Wait(); err != nil {
		_ = c.Close()
		return nil, fmt.Errorf("imap select %s: %w", folder, err)
	}
	return c, nil
}

// fetchMails connects to an IMAP server and returns the unseen messages in the
// configured folder. It always fetches with PEEK so no \Seen flags change —
// marking a message processed is the caller's job, only after a successful
// forward (see Collector.markSeen, which now applies a label rather than \Seen).
func fetchMails(src *Source, scope *Scope, collected CollectedSet) ([]Mail, error) {
	c, err := dialAndSelect(src, scope.Folder)
	if err != nil {
		return nil, err
	}
	defer func() { _ = c.Close() }()

	// Bounded by date only. UNSEEN is deliberately NOT a criterion: it is the
	// user's read state, not a work queue, and filtering on it meant the
	// collector could only ever see mail nobody had touched — three real
	// Hetzner invoices were invisible for exactly that reason (#81).
	//
	// Already-handled mail is excluded below, by its own marker.
	searchData, err := c.UIDSearch(&imap.SearchCriteria{
		Since: scope.Since,
	}, nil).Wait()
	if err != nil {
		return nil, fmt.Errorf("imap search: %w", err)
	}
	uids := searchData.AllUIDs()
	if len(uids) == 0 {
		return nil, nil
	}
	if err := scope.Check(len(uids)); err != nil {
		return nil, err
	}

	// Envelopes only. Routing needs the sender and the subject; bodies are
	// fetched afterwards for the handful that actually matched, which turns
	// tens of thousands of body fetches into a few.
	fetchOptions := &imap.FetchOptions{
		UID:      true,
		Envelope: true,
	}
	if scope.FetchBodies {
		fetchOptions.BodySection = []*imap.FetchItemBodySection{{Peek: true}} // never alter \Seen
	}
	msgs, err := c.Fetch(imap.UIDSetNum(uids...), fetchOptions).Collect()
	if err != nil {
		return nil, fmt.Errorf("imap fetch: %w", err)
	}

	mails := make([]Mail, 0, len(msgs))
	for _, msg := range msgs {
		if msg.Envelope != nil && collected.Has(msg.Envelope.MessageID) {
			continue
		}
		m, err := parseMessage(src.Name, msg)
		if err != nil {
			// Skip a malformed message rather than aborting the whole account.
			fmt.Printf("varning: kunde inte tolka uid=%d: %v\n", msg.UID, err)
			continue
		}
		mails = append(mails, m)
	}
	return mails, nil
}

// markCollected records that these messages have been handled, by copying them
// into the collector's own label.
//
// It no longer sets \Seen. That flag is the user's read state, and using it as
// a work queue fused two meanings: marking a receipt processed also marked it
// read, and skipping read mail meant the collector could never revisit anything
// (#81). A label of its own keeps the two separate.
//
// Copy rather than move: in Gmail a copy into a label ADDS the label and leaves
// the message where it is.
func markCollected(src *Source, scope *Scope, uids []uint32) error {
	if len(uids) == 0 {
		return nil
	}
	c, err := dialAndSelect(src, scope.Folder)
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }()

	imapUIDs := make([]imap.UID, len(uids))
	for i, u := range uids {
		imapUIDs[i] = imap.UID(u)
	}
	if _, err := c.Copy(imap.UIDSetNum(imapUIDs...), scope.CollectedLabel).Wait(); err != nil {
		return fmt.Errorf("imap copy to %s: %w", scope.CollectedLabel, err)
	}
	return nil
}

// loadCollected reads the Message-IDs already handled, so a run skips them.
// Keyed by Message-ID because UIDs are per-folder and change when Gmail
// re-labels a message.
func loadCollected(src *Source, scope *Scope) (CollectedSet, error) {
	out := CollectedSet{}
	c, err := dialAndSelect(src, scope.CollectedLabel)
	if err != nil {
		// The label will not exist before the first run. That is not an error:
		// nothing has been collected yet.
		return out, nil //nolint:nilerr // absent label means an empty set
	}
	defer func() { _ = c.Close() }()

	search, err := c.UIDSearch(&imap.SearchCriteria{}, nil).Wait()
	if err != nil {
		return out, nil //nolint:nilerr // an unreadable marker must not block collection
	}
	uids := search.AllUIDs()
	if len(uids) == 0 {
		return out, nil
	}
	msgs, err := c.Fetch(imap.UIDSetNum(uids...), &imap.FetchOptions{Envelope: true}).Collect()
	if err != nil {
		return out, nil //nolint:nilerr
	}
	for _, m := range msgs {
		if m.Envelope != nil && strings.TrimSpace(m.Envelope.MessageID) != "" {
			out[strings.TrimSpace(m.Envelope.MessageID)] = true
		}
	}
	return out, nil
}

// parseMessage converts a fetched IMAP message into our Mail type.
func parseMessage(_ string, msg *imapclient.FetchMessageBuffer) (Mail, error) {
	env := msg.Envelope
	if env == nil {
		return Mail{}, fmt.Errorf("uid=%d saknar envelope", msg.UID)
	}

	from := ""
	if len(env.From) > 0 {
		from = env.From[0].Addr()
	}

	var rawBody []byte
	for _, bs := range msg.BodySection {
		rawBody = bs.Bytes
		break
	}

	var attachments []Attachment
	if len(rawBody) > 0 {
		attachments, _ = extractAttachments(rawBody) // best-effort
	}

	return Mail{
		UID:         uint32(msg.UID),
		MessageID:   env.MessageID,
		From:        from,
		Subject:     env.Subject,
		Date:        env.Date,
		Body:        string(rawBody),
		Attachments: attachments,
	}, nil
}

// extractAttachments parses a raw RFC 2822 message and returns its attachments,
// with bodies decoded from their transfer encoding (base64, quoted-printable).
func extractAttachments(raw []byte) ([]Attachment, error) {
	mr, err := mail.CreateReader(bytes.NewReader(raw))
	// An unknown charset is non-fatal: the reader is still usable.
	if err != nil && !message.IsUnknownCharset(err) {
		return nil, err
	}

	var attachments []Attachment
	for {
		p, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil && !message.IsUnknownCharset(err) {
			return attachments, err
		}

		ah, ok := p.Header.(*mail.AttachmentHeader)
		if !ok {
			continue // inline text part, not an attachment
		}
		filename, _ := ah.Filename()
		if filename == "" {
			continue
		}
		data, err := io.ReadAll(p.Body)
		if err != nil {
			continue // skip an unreadable part rather than aborting
		}
		ct, _, _ := ah.ContentType()
		attachments = append(attachments, Attachment{
			Filename:    filename,
			ContentType: ct,
			Data:        data,
		})
	}

	return attachments, nil
}
