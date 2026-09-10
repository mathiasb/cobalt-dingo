package receipts

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newDuplicateTestCollector wires a Collector whose forwarded-by-hand index is
// supplied directly, so the duplicate guard can be tested without IMAP.
func newDuplicateTestCollector(dryRun bool, mails []Mail, idx ForwardedIndex, loadErr error) (*Collector, *[]uint32, *[]uint32) {
	dest := &Destination{Name: "mynt", Type: "smtp", Address: "kvitto@example.se"}
	router := NewRouter(
		[]Rule{{Name: "berget", MatchFrom: []string{"*@berget.ai"}, Destination: "mynt"}},
		[]*Destination{dest},
	)

	delivered := []uint32{}
	marked := []uint32{}
	c := &Collector{
		router: router,
		dryRun: dryRun,
		scope:  testScope(),
		fetch:  func(_ *Source, _ *Scope, _ CollectedSet) ([]Mail, error) { return mails, nil },
		loadForwarded: func(_ *Source, _ *Scope) (ForwardedIndex, error) {
			return idx, loadErr
		},
		deliverFn: func(m Mail, _ *Destination) error {
			delivered = append(delivered, m.UID)
			return nil
		},
		markSeen: func(_ *Source, _ *Scope, uids []uint32) error {
			marked = append(marked, uids...)
			return nil
		},
	}
	return c, &delivered, &marked
}

// The collector and Mathias reach the same Mynt inbox. A receipt he already
// forwarded by hand must not be forwarded again: a duplicate looks like a
// second purchase that genuinely happened and nothing flags it.
func TestProcessAccount_skipsWhatWasForwardedByHand(t *testing.T) {
	mails := []Mail{
		{UID: 1, From: "billing@berget.ai", Subject: "Receipt from Berget AB"},
		{UID: 2, From: "billing@berget.ai", Subject: "Receipt from Berget AB #2"},
	}
	idx := ForwardedIndex{"receipt from berget ab": true}

	c, delivered, marked := newDuplicateTestCollector(false, mails, idx, nil)
	res, err := c.processAccount(context.Background(), &Source{Name: "acc"})
	require.NoError(t, err)

	assert.Equal(t, []uint32{2}, *delivered, "only the receipt not already sent by hand is forwarded")
	assert.Equal(t, []uint32{2}, *marked)
	assert.Equal(t, 1, res.Routed)
	assert.Equal(t, 1, res.DuplicateCount)
	require.Len(t, res.DuplicateMails, 1)
	assert.Equal(t, uint32(1), res.DuplicateMails[0].UID)
	assert.Equal(t, 0, res.UnmatchedCount, "a duplicate is not an unmatched mail")
}

// The guard fails closed. An unreadable index is indistinguishable from an
// empty one, and an empty one means every hand-forwarded receipt goes again.
func TestProcessAccount_refusesWhenTheForwardedIndexCannotBeLoaded(t *testing.T) {
	mails := []Mail{{UID: 1, From: "billing@berget.ai", Subject: "Receipt from Berget AB"}}

	c, delivered, _ := newDuplicateTestCollector(false, mails, nil, fmt.Errorf("imap select Sent: no such folder"))
	_, err := c.processAccount(context.Background(), &Source{Name: "acc"})

	require.Error(t, err, "an unloadable duplicate guard must stop the run, not be skipped")
	assert.Contains(t, err.Error(), "no such folder")
	assert.Empty(t, *delivered, "nothing may be forwarded without a working duplicate guard")
}

// A dry run exists to be reviewed, so it must report duplicates rather than
// present them as mail it would send.
func TestProcessAccount_dryRunReportsDuplicatesWithoutForwarding(t *testing.T) {
	mails := []Mail{{UID: 1, From: "billing@berget.ai", Subject: "Fwd: Receipt from Berget AB"}}
	idx := ForwardedIndex{"receipt from berget ab": true}

	c, delivered, marked := newDuplicateTestCollector(true, mails, idx, nil)
	res, err := c.processAccount(context.Background(), &Source{Name: "acc"})
	require.NoError(t, err)

	assert.Empty(t, *delivered)
	assert.Empty(t, *marked)
	assert.Equal(t, 0, res.Routed)
	assert.Equal(t, 1, res.DuplicateCount)
}

// A collector built by NewCollector must have the guard wired. The whole defect
// class here is a guard that exists, is tested, and is never called.
func TestNewCollector_wiresTheDuplicateGuard(t *testing.T) {
	c := NewCollector(nil, NewRouter(nil, nil), true, SMTPConfig{})

	assert.NotNil(t, c.loadForwarded, "NewCollector must wire the duplicate guard")
}

// A hand-built Collector with no guard wired must refuse, not dereference nil
// and not quietly forward. This is the failure mode the guard exists for.
func TestProcessAccount_refusesWithNoDuplicateGuardAtAll(t *testing.T) {
	c := &Collector{
		router: NewRouter(nil, nil),
		scope:  testScope(),
		fetch:  func(_ *Source, _ *Scope, _ CollectedSet) ([]Mail, error) { return nil, nil },
	}

	_, err := c.processAccount(context.Background(), &Source{Name: "acc"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no duplicate guard")
}
