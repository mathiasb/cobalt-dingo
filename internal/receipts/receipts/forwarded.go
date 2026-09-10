package receipts

import (
	"fmt"
	"strings"

	"github.com/emersion/go-imap/v2"
)

// ForwardedIndex is the set of subjects already sent to a bookkeeping
// destination by hand, normalised.
//
// It exists because the collector and Mathias reach the same inboxes. He has
// forwarded 209 receipts to Mynt himself and continues to; without this the
// collector's first real run re-sends every one of them that is still in scope.
//
// A duplicate is worse than a gap. A missing receipt surfaces in reconciliation
// as an unmatched transaction; a duplicated one looks like a second purchase
// that genuinely happened, and nothing flags it.
type ForwardedIndex map[string]bool

// Has reports whether this subject has already reached bookkeeping, ignoring
// forward and reply prefixes in either language.
//
// An empty subject is never a duplicate: treating it as one would suppress
// every receipt that arrives without a subject line.
func (f ForwardedIndex) Has(subject string) bool {
	n := normaliseSubject(subject)
	if n == "" {
		return false
	}
	return f[n]
}

// add records a sent subject, ignoring ones that normalise to nothing.
func (f ForwardedIndex) add(subject string) {
	if n := normaliseSubject(subject); n != "" {
		f[n] = true
	}
}

// forwardPrefixes are stripped so a forwarded copy matches the original it came
// from. Swedish Gmail writes "VB:" and "SV:", not "Fwd:" and "Re:".
var forwardPrefixes = []string{"fwd:", "fw:", "vb:", "re:", "sv:"}

// normaliseSubject strips forward/reply prefixes and lowercases.
func normaliseSubject(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	for {
		trimmed := false
		for _, p := range forwardPrefixes {
			if strings.HasPrefix(s, p) {
				s = strings.TrimSpace(strings.TrimPrefix(s, p))
				trimmed = true
			}
		}
		if !trimmed {
			return strings.TrimSpace(s)
		}
	}
}

// loadForwardedByHand reads the subjects already sent to a bookkeeping
// destination from the account's Sent folder.
//
// Bounded by the same `since` as the run: a forward can only happen after the
// receipt it forwards arrived, so a receipt inside the window can only have
// been forwarded inside the window too.
//
// Unlike loadCollected this does NOT fail open. An unreadable Sent folder is
// indistinguishable from an empty one, and an empty index means every
// hand-forwarded receipt is sent a second time.
func (c *Collector) loadForwardedByHand(src *Source, scope *Scope) (ForwardedIndex, error) {
	out := ForwardedIndex{}

	addresses := c.router.DestinationAddresses()
	if len(addresses) == 0 {
		return nil, fmt.Errorf("duplicate guard has no destination addresses to look for: refusing to run, since an empty guard silently re-sends every receipt forwarded by hand")
	}

	client, err := dialAndSelect(src, scope.SentFolder)
	if err != nil {
		return nil, fmt.Errorf("duplicate guard: %w", err)
	}
	defer func() { _ = client.Close() }()

	// One search per destination. IMAP ANDs the criteria in a single struct, so
	// a combined "To: any of these" has to be several searches.
	for _, addr := range addresses {
		search, err := client.UIDSearch(&imap.SearchCriteria{
			Since:  scope.Since,
			Header: []imap.SearchCriteriaHeaderField{{Key: "To", Value: addr}},
		}, nil).Wait()
		if err != nil {
			return nil, fmt.Errorf("duplicate guard: search sent for %s: %w", addr, err)
		}
		uids := search.AllUIDs()
		if len(uids) == 0 {
			continue
		}
		msgs, err := client.Fetch(imap.UIDSetNum(uids...), &imap.FetchOptions{Envelope: true}).Collect()
		if err != nil {
			return nil, fmt.Errorf("duplicate guard: fetch sent for %s: %w", addr, err)
		}
		for _, m := range msgs {
			if m.Envelope != nil {
				out.add(m.Envelope.Subject)
			}
		}
	}
	return out, nil
}
