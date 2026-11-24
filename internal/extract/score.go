package extract

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// Truth is the ground truth for one synthetic invoice, as written by
// scripts/gen-synthetic-invoices.py. Amounts are decimal strings.
type Truth struct {
	File            string `json:"file"`
	Supplier        string `json:"supplier"`
	InvoiceNumber   string `json:"invoice_number"`
	InvoiceDate     string `json:"invoice_date"`
	DueDate         string `json:"due_date"`
	Currency        string `json:"currency"`
	Total           string `json:"total"`
	VATAmount       string `json:"vat_amount"`
	ExpectedAccount int    `json:"expected_account"`
}

// FieldScore is how many fields of one invoice were read correctly.
type FieldScore struct {
	Fields int
	Right  int
	Wrong  []string // names of the fields that differ

	// RightFields names the labelled fields that matched, so a per-field report
	// can count each field over only the invoices that label it.
	RightFields []string
}

// Score compares a reading with the truth, field by field. Only fields the
// truth labels are scored: a public dataset that has no invoice number cannot
// make that field right or wrong. The supplier matches ignoring case, or when
// the reading is a word-boundary prefix of the truth (some datasets give the
// seller as name plus address); every other field matches exactly. A truth
// amount that does not parse counts as wrong, never as a match.
func Score(t Truth, got Invoice) FieldScore {
	totalWant, errT := ParseAmount(t.Total)
	vatWant, errV := ParseAmount(t.VATAmount)
	checks := []struct {
		name     string
		labelled bool
		ok       bool
	}{
		{"supplier", t.Supplier != "", supplierMatches(t.Supplier, got.Supplier)},
		{"invoice_number", t.InvoiceNumber != "", got.InvoiceNumber == t.InvoiceNumber},
		{"invoice_date", t.InvoiceDate != "", got.InvoiceDate == t.InvoiceDate},
		{"due_date", t.DueDate != "", got.DueDate == t.DueDate},
		{"currency", t.Currency != "", got.Currency == t.Currency},
		{"total", t.Total != "", errT == nil && got.Total == totalWant},
		{"vat_amount", t.VATAmount != "", errV == nil && got.VATAmount == vatWant},
	}
	var s FieldScore
	for _, c := range checks {
		if !c.labelled {
			continue
		}
		s.Fields++
		if c.ok {
			s.Right++
			s.RightFields = append(s.RightFields, c.name)
		} else {
			s.Wrong = append(s.Wrong, c.name)
		}
	}
	return s
}

// minSupplierPrefix stops a one- or two-letter reading matching everything.
const minSupplierPrefix = 3

func supplierMatches(truth, got string) bool {
	w := strings.ToLower(strings.TrimSpace(truth))
	g := strings.ToLower(strings.TrimSpace(got))
	if g == "" {
		return false
	}
	if g == w {
		return true
	}
	if len(g) < minSupplierPrefix || !strings.HasPrefix(w, g) {
		return false
	}
	next, _ := utf8.DecodeRuneInString(w[len(g):])
	return !unicode.IsLetter(next) && !unicode.IsDigit(next)
}
