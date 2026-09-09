package receipts

import (
	"fmt"
	"time"
)

// maxWindow is the longest search window allowed. A ten-year "bound" is the
// unbounded search wearing a date, and the unbounded search is what made the
// collector unusable: 41,000 unread messages, full bodies, against Gmail's
// 2,500 MB/day IMAP ceiling (#79).
const maxWindow = 400 * 24 * time.Hour

// ScopeConfig bounds a collection run.
type ScopeConfig struct {
	// Since is the date floor. Required: UNSEEN alone is the wrong queue
	// signal when unread is the natural resting state of a mailbox.
	Since time.Time

	// Max is the most messages a single run may process. A cap that truncates
	// would forward some receipts and abandon the rest with no signal, so
	// exceeding it is an error.
	Max int

	// FetchBodies pulls full message bodies during the search. Off by default:
	// routing needs only sender and subject, and bodies are fetched afterwards
	// for the handful that matched.
	FetchBodies bool
}

// Scope is a validated collection bound.
type Scope struct {
	Since       time.Time
	Max         int
	FetchBodies bool
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
	return &Scope{Since: cfg.Since, Max: cfg.Max, FetchBodies: cfg.FetchBodies}, nil
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
