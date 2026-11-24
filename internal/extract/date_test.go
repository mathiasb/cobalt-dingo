package extract_test

import (
	"testing"

	"github.com/mathiasb/cobalt-dingo/internal/extract"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseDate_ResolvesByTheGivenOrderAndFlagsWhenBothReadingsAreValid(t *testing.T) {
	tests := []struct {
		name          string
		text          string
		order         extract.DateOrder
		wantISO       string
		wantAmbiguous bool
	}{
		{"iso is never ambiguous", "2026-11-02", extract.DMY, "2026-11-02", false},
		{"iso with slashes", "2026/11/02", extract.MDY, "2026-11-02", false},
		{"equal parts cannot swap", "11.11.2026", extract.DMY, "2026-11-11", false},
		{"day first, both readings valid", "02.11.2026", extract.DMY, "2026-11-02", true},
		{"month first, both readings valid", "02.11.2026", extract.MDY, "2026-02-11", true},
		{"day over 12 forces day first even when order says month first", "18/09/2015", extract.MDY, "2015-09-18", false},
		{"day over 12 in second place forces month first", "09/18/2015", extract.DMY, "2015-09-18", false},
		{"two digit year", "02/11/26", extract.DMY, "2026-11-02", true},
		{"dashes", "02-11-2026", extract.DMY, "2026-11-02", true},
		{"day month-name year", "02 Nov 2026", extract.MDY, "2026-11-02", false},
		{"swedish month name", "2 november 2026", extract.MDY, "2026-11-02", false},
		{"month name first", "November 2, 2026", extract.DMY, "2026-11-02", false},
		{"short month name first", "Nov 2, 2026", extract.DMY, "2026-11-02", false},
		{"surrounding space", "  2026-11-02 ", extract.DMY, "2026-11-02", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := extract.ParseDate(tt.text, tt.order)

			require.NoError(t, err)
			assert.Equal(t, tt.wantISO, got.ISO)
			assert.Equal(t, tt.wantAmbiguous, got.Ambiguous)
		})
	}
}

func TestParseDate_RejectsTextThatIsNotADate(t *testing.T) {
	for _, text := range []string{"", "sometime in May", "32/13/2026", "31.02.2026", "2026-13-01", "11/2026"} {
		t.Run(text, func(t *testing.T) {
			_, err := extract.ParseDate(text, extract.DMY)

			require.Error(t, err)
		})
	}
}
