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
