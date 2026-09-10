package receipts_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mathiasb/cobalt-dingo/internal/receipts/receipts"
)

// The collector searched UNSEEN with no date bound and fetched full bodies for
// every hit. Against a real mailbox that is 41,000 messages and gigabytes of
// download — Gmail's IMAP ceiling is 2,500 MB/day — and the first live attempt
// had to be killed after ten minutes with no output (#79).
//
// UNSEEN is the wrong queue signal when unread is the natural resting state of
// 41,000 messages.
func TestScopeRequiresABound(t *testing.T) {
	_, err := receipts.NewScope(receipts.ScopeConfig{})
	require.Error(t, err, "an unbounded scope must be refused, not defaulted to everything")
	assert.Contains(t, err.Error(), "since")
}

func TestScopeRejectsAnAbsurdWindow(t *testing.T) {
	_, err := receipts.NewScope(receipts.ScopeConfig{
		Since: time.Now().AddDate(-10, 0, 0),
		Max:   500,
	})
	require.Error(t, err, "a ten-year window is the unbounded search wearing a date")
}

// A cap is not a truncation. Silently processing the first N of a larger set
// would forward some receipts and abandon the rest with no signal.
func TestOverTheCapRefusesRatherThanTruncates(t *testing.T) {
	s, err := receipts.NewScope(receipts.ScopeConfig{
		Since: time.Now().AddDate(0, -2, 0),
		Max:   10,
	})
	require.NoError(t, err)

	err = s.Check(47)
	require.Error(t, err, "more candidates than the cap must refuse")
	assert.Contains(t, err.Error(), "47")
	assert.Contains(t, err.Error(), "10", "the error must name both numbers so the operator can widen deliberately")
}

func TestWithinTheCapPasses(t *testing.T) {
	s, err := receipts.NewScope(receipts.ScopeConfig{
		Since: time.Now().AddDate(0, -2, 0),
		Max:   10,
	})
	require.NoError(t, err)
	assert.NoError(t, s.Check(4), "the four-message test case must pass")
	assert.NoError(t, s.Check(0), "nothing to do is not an error")
}

// Routing needs the sender and the subject. Bodies are needed only for the
// handful that match, which turns 41,000 body fetches into four.
func TestScopeDefaultsToEnvelopeOnly(t *testing.T) {
	s, err := receipts.NewScope(receipts.ScopeConfig{
		Since: time.Now().AddDate(0, -2, 0),
		Max:   100,
	})
	require.NoError(t, err)
	assert.False(t, s.FetchBodies, "bodies must be fetched only for matched mail, never for the whole search")
}

// The collector searched UNSEEN in INBOX, so it could only ever see mail
// Mathias had never touched — which is the mail LEAST likely to be a receipt he
// cares about. Three real Hetzner invoices were invisible to every scan in the
// session because he had read them and Gmail had labelled them out of the inbox
// (#81).
//
// The folder is no longer named here. Naming it was the second half of the same
// bug: "[Gmail]/All Mail" does not exist on every one of these accounts, since
// Gmail localises it. An empty Folder now means "discover the mailbox flagged
// \All", which is INBOX on no account anywhere.
func TestScopeLeavesTheSearchFolderToDiscovery(t *testing.T) {
	s, err := receipts.NewScope(receipts.ScopeConfig{
		Since: time.Now().AddDate(0, -2, 0),
		Max:   50,
	})
	require.NoError(t, err)
	assert.Empty(t, s.Folder,
		"an unset folder must mean discover \\All, not fall back to a localised name or INBOX")
}

func TestScopeFolderIsConfigurable(t *testing.T) {
	s, err := receipts.NewScope(receipts.ScopeConfig{
		Since: time.Now().AddDate(0, -2, 0), Max: 50, Folder: "INBOX",
	})
	require.NoError(t, err)
	assert.Equal(t, "INBOX", s.Folder)
}

// \Seen is the user's read state. The collector was using it as its own work
// queue, which fused two meanings: marking a receipt processed also marked it
// read, and refusing to look at read mail meant it could never revisit anything.
//
// Processed-ness now lives in its own label, leaving \Seen to mean what it means
// to Mathias.
func TestCollectedMarkerIsALabelNotTheSeenFlag(t *testing.T) {
	s, err := receipts.NewScope(receipts.ScopeConfig{
		Since: time.Now().AddDate(0, -2, 0), Max: 50,
	})
	require.NoError(t, err)
	assert.NotEmpty(t, s.CollectedLabel, "processed-ness needs a durable marker of its own")
	assert.NotContains(t, s.CollectedLabel, "Seen")
}

// Already-collected mail must be skipped by identity that survives re-labelling
// and folder moves. UIDs do not: they are per-folder and change on move.
func TestAlreadyCollectedAreSkippedByMessageID(t *testing.T) {
	seen := receipts.CollectedSet{"<abc@hetzner.com>": true}

	assert.True(t, seen.Has("<abc@hetzner.com>"))
	assert.True(t, seen.Has("  <abc@hetzner.com>  "), "whitespace must not defeat the check")
	assert.False(t, seen.Has("<other@hetzner.com>"))
	assert.False(t, seen.Has(""), "a message with no id must never count as collected")
}
