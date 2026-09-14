package fortnox

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFlexString_acceptsStringOrNumber(t *testing.T) {
	tests := []struct {
		json string
		want string
	}{
		{`"2026/A-114"`, "2026/A-114"},
		{`"9001"`, "9001"},
		{`9001`, "9001"},
		{`0`, "0"},
		{`""`, ""},
		{`null`, ""},
	}
	for _, tt := range tests {
		t.Run(tt.json, func(t *testing.T) {
			var f FlexString
			require.NoError(t, json.Unmarshal([]byte(tt.json), &f))
			assert.Equal(t, tt.want, string(f))
		})
	}
}

// An integer must not acquire a decimal point on the way through — a reference
// of "9001" and one of "9001.0" are different strings to a human reading a
// payment.
func TestFlexString_integerKeepsItsLiteralForm(t *testing.T) {
	var f FlexString
	require.NoError(t, json.Unmarshal([]byte(`9001`), &f))
	assert.Equal(t, "9001", string(f))
}

func TestFlexString_structuredValuesAreAnError(t *testing.T) {
	for _, bad := range []string{`{}`, `[]`, `true`} {
		var f FlexString
		assert.Error(t, json.Unmarshal([]byte(bad), &f), "input %s", bad)
	}
}
