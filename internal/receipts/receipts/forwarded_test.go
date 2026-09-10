package receipts

import (
	"testing"

	"github.com/stretchr/testify/assert"
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
