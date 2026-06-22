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
