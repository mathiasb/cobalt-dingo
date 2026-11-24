package extract_test

import (
	"testing"

	"github.com/mathiasb/cobalt-dingo/internal/extract"
	"github.com/stretchr/testify/assert"
)

func truth() extract.Truth {
	return extract.Truth{
		Supplier: "Fjällresor AB", InvoiceNumber: "56260004", InvoiceDate: "2026-11-11",
		DueDate: "2026-12-11", Currency: "SEK", Total: "3592.39", VATAmount: "203.34",
	}
}

func TestScore_AllFieldsRightWhenTheReadingMatchesTruth(t *testing.T) {
	got := extract.Invoice{
		Supplier: "Fjällresor AB", InvoiceNumber: "56260004", InvoiceDate: "2026-11-11",
		DueDate: "2026-12-11", Currency: "SEK", Total: 359239, VATAmount: 20334,
	}

	s := extract.Score(truth(), got)

	assert.Equal(t, 7, s.Right)
	assert.Equal(t, 7, s.Fields)
	assert.Empty(t, s.Wrong)
	assert.Len(t, s.RightFields, 7)
}

func TestScore_NamesEachWrongFieldAndComparesSupplierIgnoringCaseAndSpace(t *testing.T) {
	got := extract.Invoice{
		Supplier: "  fjällresor ab ", InvoiceNumber: "56260005", InvoiceDate: "2026-11-11",
		DueDate: "2026-12-11", Currency: "SEK", Total: 359240, VATAmount: 20334,
	}

	s := extract.Score(truth(), got)

	assert.Equal(t, 5, s.Right)
	assert.ElementsMatch(t, []string{"invoice_number", "total"}, s.Wrong)
}

// Public datasets do not label every field (SROIE receipts have no invoice
// number or VAT; katanaml invoices have no due date). An unlabelled field is
// not scored, so it can neither inflate nor deflate accuracy.
func TestScore_SkipsFieldsTheTruthDoesNotLabel(t *testing.T) {
	tr := extract.Truth{Supplier: "Fjällresor AB", InvoiceDate: "2026-11-11", Total: "3592.39"}
	got := extract.Invoice{Supplier: "Fjällresor AB", InvoiceNumber: "anything", InvoiceDate: "2026-11-11", Total: 359239, VATAmount: 999}

	s := extract.Score(tr, got)

	assert.Equal(t, 3, s.Fields, "only supplier, invoice_date and total are labelled")
	assert.Equal(t, 3, s.Right)
	assert.Empty(t, s.Wrong)
	assert.ElementsMatch(t, []string{"supplier", "invoice_date", "total"}, s.RightFields,
		"a per-field report must count only labelled fields in its denominator")
}

// Some datasets give the seller as name plus address in one string.
func TestScore_AcceptsASupplierThatIsAWordBoundaryPrefixOfTheTruth(t *testing.T) {
	tests := []struct {
		name, truth, got string
		want             bool
	}{
		{"name then address", "Bradley-Andrade 9879 Elizabeth Common Lake Jonathan", "Bradley-Andrade", true},
		{"case differs", "Smith Ltd 12 Main St", "smith ltd", true},
		{"cut mid word", "Bradley-Andrade 9879 Elizabeth", "Bradley-And", false},
		{"different supplier", "Bradley-Andrade 9879 Elizabeth", "Bradley Ltd", false},
		{"too short to mean anything", "A Plus Consulting 12 Main St", "A", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := extract.Score(extract.Truth{Supplier: tt.truth}, extract.Invoice{Supplier: tt.got})

			assert.Equal(t, 1, s.Fields)
			assert.Equal(t, tt.want, s.Right == 1)
		})
	}
}
