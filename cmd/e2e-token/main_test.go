package main

import "testing"

// Fortnox rotates the refresh token on every use, so "did it rotate" is the
// only signal that a write-back is needed. Getting this wrong is expensive in
// both directions: a false negative loses the rotated token and forces a
// re-auth (#37); a false positive writes an identical row and, worse, would
// clobber a token the running pod may have rotated in the meantime.
func TestShouldWriteBack(t *testing.T) {
	tests := []struct {
		name       string
		oldRefresh string
		newRefresh string
		want       bool
	}{
		{"rotated", "old-rt", "new-rt", true},
		{"unchanged", "same-rt", "same-rt", false},
		{"unchanged despite trailing newline from the file read", "same-rt\n", "same-rt", false},
		{"unchanged despite surrounding whitespace", "  same-rt  ", "same-rt", false},
		{"empty new token is never written back", "old-rt", "", false},
		{"whitespace-only new token is never written back", "old-rt", "   \n", false},
		{"first run has no prior token", "", "new-rt", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldWriteBack(tt.oldRefresh, tt.newRefresh); got != tt.want {
				t.Errorf("shouldWriteBack(%q, %q) = %v, want %v",
					tt.oldRefresh, tt.newRefresh, got, tt.want)
			}
		})
	}
}
