package receipts

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testRouter() *Router {
	dests := []*Destination{
		{Name: "mynt", Type: "smtp", Address: "kvitto+x@mynt.se"},
		{Name: "fortnox-invoices", Type: "fortnox-invoices", Address: ""},
	}
	rules := []Rule{
		{Name: "mynt", MatchFrom: []string{"noreply@mynt.se"}, Destination: "mynt"},
		{Name: "leverantor", MatchFrom: []string{"*@bahnhof.se"}, Destination: "fortnox-invoices"},
		{Name: "amne", MatchSubject: []string{"faktura"}, Destination: "fortnox-invoices"},
	}
	return NewRouter(rules, dests)
}

func TestRoute(t *testing.T) {
	r := testRouter()

	tests := []struct {
		name     string
		mail     Mail
		wantDest string // "" means no match
	}{
		{"exact from", Mail{From: "noreply@mynt.se"}, "mynt"},
		{"wildcard domain", Mail{From: "billing@bahnhof.se"}, "fortnox-invoices"},
		{"subject match", Mail{From: "x@unknown.com", Subject: "Din Faktura 2026"}, "fortnox-invoices"},
		{"no match", Mail{From: "x@unknown.com", Subject: "hej"}, ""},
		{"first rule wins", Mail{From: "noreply@mynt.se", Subject: "faktura"}, "mynt"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dest, ok := r.Route(tc.mail)
			if tc.wantDest == "" {
				assert.False(t, ok)
				return
			}
			require.True(t, ok)
			assert.Equal(t, tc.wantDest, dest.Name)
		})
	}
}

func TestMatchesAddress(t *testing.T) {
	assert.True(t, matchesAddress("a@MYNT.se", "a@mynt.se"), "case-insensitive exact")
	assert.True(t, matchesAddress("billing@bahnhof.se", "*@bahnhof.se"), "wildcard domain")
	assert.False(t, matchesAddress("a@other.se", "*@bahnhof.se"))
	assert.False(t, matchesAddress("a@bahnhof.se.evil.com", "*@bahnhof.se"))
}

// A rule that names BOTH a sender and a subject means "from this sender AND
// about this" — that is how every rule in the example config is written, e.g.
//
//   - id: hetzner-invoice
//     match:
//     from_domain: hetzner.com
//     subject_contains: "Invoice"
//
// ruleMatches ORs the two criteria instead, so such a rule fires on the subject
// alone. Every invoice-shaped mail from anyone would route to Fortnox as a
// supplier invoice, and the sender constraint would be decorative.
func TestRuleWithBothCriteriaRequiresBoth(t *testing.T) {
	r := NewRouter(
		[]Rule{{
			Name:         "hetzner-invoice",
			MatchFrom:    []string{"*@hetzner.com"},
			MatchSubject: []string{"Invoice"},
			Destination:  "fortnox-invoices",
		}},
		[]*Destination{{Name: "fortnox-invoices", Type: "fortnox-invoices"}},
	)

	t.Run("both criteria met routes", func(t *testing.T) {
		d, ok := r.Route(Mail{From: "billing@hetzner.com", Subject: "Your Invoice 2026"})
		require.True(t, ok)
		assert.Equal(t, "fortnox-invoices", d.Name)
	})

	t.Run("subject alone must not route", func(t *testing.T) {
		_, ok := r.Route(Mail{From: "stranger@example.com", Subject: "Your Invoice 2026"})
		assert.False(t, ok,
			"a rule naming a sender must not match mail from anyone else")
	})

	t.Run("sender alone must not route", func(t *testing.T) {
		_, ok := r.Route(Mail{From: "billing@hetzner.com", Subject: "Scheduled maintenance"})
		assert.False(t, ok,
			"a rule naming a subject must not match unrelated mail from that sender")
	})
}

// A rule naming only one criterion stays unconstrained on the other, so
// single-criterion rules must keep working exactly as before.
func TestSingleCriterionRulesStillMatch(t *testing.T) {
	r := testRouter()

	d, ok := r.Route(Mail{From: "billing@bahnhof.se", Subject: "anything at all"})
	require.True(t, ok, "a from-only rule must ignore the subject")
	assert.Equal(t, "fortnox-invoices", d.Name)

	d, ok = r.Route(Mail{From: "someone@nowhere.example", Subject: "Din Faktura 2026"})
	require.True(t, ok, "a subject-only rule must ignore the sender")
	assert.Equal(t, "fortnox-invoices", d.Name)
}
