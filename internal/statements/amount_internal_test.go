package statements

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseDecimalMinor(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    int64
		wantErr bool
	}{
		{"two decimals", "325.50", 32550, false},
		{"whole number no point", "1000", 100000, false},
		{"one decimal is padded", "12.5", 1250, false},
		{"zero", "0.00", 0, false},
		{"negative", "-99.99", -9999, false},
		{"explicit plus", "+10.00", 1000, false},
		{"surrounding whitespace", "  42.00\n", 4200, false},
		{"trailing zeros beyond minor units are harmless", "10.500", 1050, false},
		{"large amount", "1234567.89", 123456789, false},

		// Rejected rather than rounded. A bank that emits real sub-cent
		// precision means our assumption about the currency exponent is wrong,
		// and silently rounding it would corrupt the balance identity.
		{"real precision beyond minor units", "10.505", 0, true},
		{"empty", "", 0, true},
		{"not a number", "abc", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseDecimalMinor(tt.in)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// The naive float route is what this function exists to avoid. Pinned as a
// table entry rather than a clever comparison: 8.87 is not exactly
// representable in float64, so int64(f*100) truncates to 886.
func TestParseDecimalMinor_ExactWhereNaiveFloatTruncates(t *testing.T) {
	got, err := parseDecimalMinor("8.87")
	require.NoError(t, err)
	assert.Equal(t, int64(887), got)
	assert.NotEqual(t, int64(886), got,
		"if this ever reads 886, someone replaced the exact parse with int64(f*100)")
}
