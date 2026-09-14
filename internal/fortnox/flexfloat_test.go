package fortnox

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Fortnox is inconsistent about amount types ACROSS endpoints, not just
// across versions: /3/supplierinvoices sends Total and Balance as strings,
// /3/invoices sends them as numbers. Verified live 2026-09-14 with
// cmd/fortnox-shape.
//
// A plain float64 field decodes a JSON string to zero without erroring, which
// is how a supplier invoice amount becomes SEK 0.00 silently.
func TestFlexFloat_acceptsStringOrNumber(t *testing.T) {
	tests := []struct {
		name string
		json string
		want float64
	}{
		{"bare number", `12500.50`, 12500.50},
		{"quoted string", `"12500.50"`, 12500.50},
		{"quoted integer", `"12500"`, 12500},
		{"zero", `0`, 0},
		{"quoted zero", `"0.00"`, 0},
		{"negative string", `"-1500.25"`, -1500.25},
		{"empty string is zero", `""`, 0},
		{"null is zero", `null`, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var f FlexFloat
			require.NoError(t, json.Unmarshal([]byte(tt.json), &f))
			assert.InDelta(t, tt.want, float64(f), 0.0001)
		})
	}
}

// Anything else must ERROR rather than silently becoming zero. A zero amount
// that should have been a number is exactly the failure this type exists to
// prevent, and swallowing a parse error reintroduces it.
func TestFlexFloat_unparseableIsAnError(t *testing.T) {
	for _, bad := range []string{`"twelve"`, `"1 200,50"`, `{}`, `[]`, `true`} {
		var f FlexFloat
		assert.Error(t, json.Unmarshal([]byte(bad), &f), "input %s must not decode to zero", bad)
	}
}
