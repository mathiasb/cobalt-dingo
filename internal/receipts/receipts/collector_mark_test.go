package receipts

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestCollector builds a Collector wired to in-memory fakes so processAccount
// can be tested without a live IMAP/SMTP server. It records which UIDs get
// marked \Seen and whether deliver was called.
func newTestCollector(dryRun bool, mails []Mail, failUID uint32) (*Collector, *[]uint32, *[]uint32) {
	dest := &Destination{Name: "self", Type: "smtp", Address: "me@example.com"}
	router := NewRouter(
		[]Rule{{Name: "adlibris", MatchFrom: []string{"info@adlibris.com"}, Destination: "self"}},
		[]*Destination{dest},
	)

	delivered := []uint32{}
	marked := []uint32{}
	c := &Collector{
		router: router,
		dryRun: dryRun,
		fetch:  func(_ *Source, _ *Scope, _ CollectedSet) ([]Mail, error) { return mails, nil },
		scope:  testScope(),
		deliverFn: func(m Mail, _ *Destination) error {
			delivered = append(delivered, m.UID)
			if m.UID == failUID {
				return fmt.Errorf("smtp boom uid=%d", m.UID)
			}
			return nil
		},
		markSeen: func(_ *Source, _ *Scope, uids []uint32) error {
			marked = append(marked, uids...)
			return nil
		},
	}
	return c, &delivered, &marked
}

func TestProcessAccount_marksOnlyForwardedUIDs(t *testing.T) {
	mails := []Mail{
		{UID: 1, From: "info@adlibris.com"}, // matches → delivered OK → marked
		{UID: 2, From: "info@adlibris.com"}, // matches → deliver fails → NOT marked
		{UID: 3, From: "noise@other.com"},   // no rule → not delivered, not marked
	}
	c, delivered, marked := newTestCollector(false, mails, 2)

	res, err := c.processAccount(context.Background(), &Source{Name: "acc"})
	require.NoError(t, err)

	assert.Equal(t, []uint32{1, 2}, *delivered, "both matching mails attempt delivery")
	assert.Equal(t, []uint32{1}, *marked, "only the successfully forwarded mail is marked \\Seen")
	assert.Equal(t, 1, res.Routed)
	assert.Len(t, res.Errors, 1, "the failed forward is recorded")
	assert.Equal(t, 1, res.UnmatchedCount)
}

func TestProcessAccount_dryRun_marksAndSendsNothing(t *testing.T) {
	mails := []Mail{
		{UID: 1, From: "info@adlibris.com"},
		{UID: 2, From: "info@adlibris.com"},
	}
	c, delivered, marked := newTestCollector(true, mails, 0)

	res, err := c.processAccount(context.Background(), &Source{Name: "acc"})
	require.NoError(t, err)

	assert.Empty(t, *delivered, "dry run forwards nothing")
	assert.Empty(t, *marked, "dry run marks nothing \\Seen")
	assert.Equal(t, 2, res.Routed, "dry run still counts what would route")
}

// testScope is the minimum valid bound; Run refuses without one.
func testScope() *Scope {
	s, err := NewScope(ScopeConfig{Since: time.Now().AddDate(0, -1, 0), Max: 100})
	if err != nil {
		panic(err)
	}
	return s
}
