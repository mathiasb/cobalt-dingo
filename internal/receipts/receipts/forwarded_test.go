package receipts

import (
	"testing"

	"github.com/emersion/go-imap/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestForwardedIndex_matchesAcrossForwardAndReplyPrefixes(t *testing.T) {
	idx := ForwardedIndex{"kvitto från berget ab": true}

	for _, subject := range []string{
		"Kvitto från Berget AB",
		"Fwd: Kvitto från Berget AB",
		"VB: Kvitto från Berget AB",
		"Vb: Sv: Kvitto från Berget AB",
		"  FWD:   Kvitto från Berget AB  ",
	} {
		assert.True(t, idx.Has(subject), "subject %q should be recognised as already forwarded", subject)
	}
}

func TestForwardedIndex_doesNotMatchADifferentSubject(t *testing.T) {
	idx := ForwardedIndex{"kvitto från berget ab": true}

	assert.False(t, idx.Has("Kvitto från Hetzner"))
}

// An empty subject must never be treated as a duplicate: it would silently
// suppress every receipt that arrives without one.
func TestForwardedIndex_emptySubjectIsNeverADuplicate(t *testing.T) {
	idx := ForwardedIndex{"": true}

	assert.False(t, idx.Has(""))
	assert.False(t, idx.Has("   "))
	assert.False(t, idx.Has("Fwd:"))
}

// Gmail localises folder names — one of these three accounts has
// "[Gmail]/Skickat" and another has "[Gmail]/Sent Mail". Guessing the name is
// what broke the first live dry run, so the folder is discovered by its
// RFC 6154 \Sent attribute instead.
func TestPickSpecialFolder_findsItByAttributeNotName(t *testing.T) {
	boxes := []*imap.ListData{
		{Mailbox: "INBOX", Attrs: []imap.MailboxAttr{}},
		{Mailbox: "[Gmail]/Skickat", Attrs: []imap.MailboxAttr{imap.MailboxAttrSent}},
		{Mailbox: "[Gmail]/Papperskorgen", Attrs: []imap.MailboxAttr{imap.MailboxAttrTrash}},
	}

	got, err := pickSpecialFolder(boxes, imap.MailboxAttrSent)

	require.NoError(t, err)
	assert.Equal(t, "[Gmail]/Skickat", got)
}

func TestPickSpecialFolder_worksForAnEnglishAccountToo(t *testing.T) {
	boxes := []*imap.ListData{
		{Mailbox: "[Gmail]/Sent Mail", Attrs: []imap.MailboxAttr{imap.MailboxAttrSent}},
	}

	got, err := pickSpecialFolder(boxes, imap.MailboxAttrSent)

	require.NoError(t, err)
	assert.Equal(t, "[Gmail]/Sent Mail", got)
}

// No \Sent folder means no duplicate guard, and no duplicate guard means every
// hand-forwarded receipt is sent again. Refuse rather than fall back to a name.
func TestPickSpecialFolder_refusesWhenNoFolderIsMarkedSent(t *testing.T) {
	boxes := []*imap.ListData{
		{Mailbox: "INBOX"},
		{Mailbox: "Sent"}, // looks right, carries no attribute — not good enough
	}

	_, err := pickSpecialFolder(boxes, imap.MailboxAttrSent)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "\\Sent")
}

// All Mail is localised exactly like Sent, and the same guess broke on the
// second account: it calls the folder something other than "[Gmail]/All Mail".
func TestPickSpecialFolder_findsAllMailByAttribute(t *testing.T) {
	boxes := []*imap.ListData{
		{Mailbox: "INBOX"},
		{Mailbox: "[Gmail]/Alla mail", Attrs: []imap.MailboxAttr{imap.MailboxAttrAll}},
		{Mailbox: "[Gmail]/Skickat", Attrs: []imap.MailboxAttr{imap.MailboxAttrSent}},
	}

	got, err := pickSpecialFolder(boxes, imap.MailboxAttrAll)

	require.NoError(t, err)
	assert.Equal(t, "[Gmail]/Alla mail", got)
}
