package receipts

import (
	"fmt"
	"strings"
	"time"
)

// maxWindow is the longest search window allowed. A ten-year "bound" is the
// unbounded search wearing a date, and the unbounded search is what made the
// collector unusable: 41,000 unread messages, full bodies, against Gmail's
// 2,500 MB/day IMAP ceiling (#79).
const maxWindow = 400 * 24 * time.Hour

const (
	// defaultFolder is All Mail, not INBOX. Read and archived receipts are
	// still unrouted receipts.
	defaultFolder = "[Gmail]/All Mail"

	// defaultCollectedLabel keeps processed-ness out of the user's read state.
	defaultCollectedLabel = "cobalt-dingo/collected"

	// defaultSentFolder is where the duplicate guard looks for receipts already
	// forwarded by hand. Gmail localises this name — these accounts are Swedish,
	// hence "Skickat". A wrong value is not silent: loadForwardedByHand fails the
	// run rather than returning an empty index.
	defaultSentFolder = "[Gmail]/Skickat"
)

// ScopeConfig bounds a collection run.
type ScopeConfig struct {
	// Since is the date floor. Required: UNSEEN alone is the wrong queue
	// signal when unread is the natural resting state of a mailbox.
	Since time.Time

	// Max is the most messages a single run may process. A cap that truncates
	// would forward some receipts and abandon the rest with no signal, so
	// exceeding it is an error.
	Max int

	// Folder to search. Defaults to All Mail rather than INBOX: a receipt that
	// has been read or archived is still an unrouted receipt, and searching
	// INBOX meant the collector could only see mail nobody had touched (#81).
	Folder string

	// CollectedLabel marks mail this tool has already handled. Defaults to a
	// label of its own rather than the \Seen flag — \Seen is the user's read
	// state, and overloading it fused two meanings: marking a receipt processed
	// also marked it read, and skipping read mail meant never revisiting
	// anything.
	CollectedLabel string

	// FetchBodies pulls full message bodies during the search. Off by default:
	// routing needs only sender and subject, and bodies are fetched afterwards
	// for the handful that matched.
	FetchBodies bool

	// SentFolder is searched for receipts already forwarded by hand, so the
	// collector does not send them a second time. Defaults to Gmail's Swedish
	// Sent folder.
	SentFolder string
}

// Scope is a validated collection bound.
type Scope struct {
	Since          time.Time
	Max            int
	Folder         string
	CollectedLabel string
	FetchBodies    bool
	SentFolder     string
}

// NewScope validates a bound, refusing rather than defaulting.
//
// Defaulting an absent bound to "everything" is what produced the original
// defect: the design was correct for a tidy mailbox and catastrophic for a real
// one, and nothing in between reported a problem.
func NewScope(cfg ScopeConfig) (*Scope, error) {
	if cfg.Since.IsZero() {
		return nil, fmt.Errorf("scope has no `since` date: refusing to search unbounded, which against a real backlog means tens of thousands of messages")
	}
	if window := time.Since(cfg.Since); window > maxWindow {
		return nil, fmt.Errorf("scope window of %.0f days exceeds the %.0f-day maximum: widen deliberately in config rather than by accident",
			window.Hours()/24, maxWindow.Hours()/24)
	}
	if cfg.Max <= 0 {
		return nil, fmt.Errorf("scope has no message cap: a first run over a long window would forward hundreds unattended")
	}
	folder := cfg.Folder
	if folder == "" {
		folder = defaultFolder
	}
	label := cfg.CollectedLabel
	if label == "" {
		label = defaultCollectedLabel
	}
	sent := cfg.SentFolder
	if sent == "" {
		sent = defaultSentFolder
	}
	return &Scope{
		Since: cfg.Since, Max: cfg.Max,
		Folder: folder, CollectedLabel: label,
		FetchBodies: cfg.FetchBodies,
		SentFolder:  sent,
	}, nil
}

// Check reports whether a candidate count is safe to process.
//
// Over the cap is an error, never a truncation: processing the first N of a
// larger set forwards some receipts and abandons the rest silently, which is
// indistinguishable from success.
func (s *Scope) Check(candidates int) error {
	if candidates > s.Max {
		return fmt.Errorf("%d candidate message(s) exceeds the per-run cap of %d: narrow the window or raise the cap deliberately — truncating would forward some receipts and silently abandon the rest",
			candidates, s.Max)
	}
	return nil
}

// CollectedSet is the set of Message-IDs this tool has already handled.
//
// Keyed by Message-ID rather than UID: UIDs are per-folder and change when a
// message moves, so a UID-keyed record silently stops matching the moment Gmail
// re-labels something.
type CollectedSet map[string]bool

// Has reports whether a message has already been collected. An empty id is
// never collected — a message without one must be reconsidered rather than
// silently skipped.
func (c CollectedSet) Has(messageID string) bool {
	id := strings.TrimSpace(messageID)
	if id == "" {
		return false
	}
	return c[id]
}
