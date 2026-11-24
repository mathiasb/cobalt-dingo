package extract

import "strings"

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
}

// Score compares a reading with the truth, field by field. The supplier is
// compared ignoring case and surrounding space; every other field exactly.
// A truth amount that does not parse counts as wrong, never as a match.
func Score(t Truth, got Invoice) FieldScore {
	totalWant, errT := ParseAmount(t.Total)
	vatWant, errV := ParseAmount(t.VATAmount)
	checks := []struct {
		name string
		ok   bool
	}{
		{"supplier", strings.EqualFold(strings.TrimSpace(got.Supplier), strings.TrimSpace(t.Supplier))},
		{"invoice_number", got.InvoiceNumber == t.InvoiceNumber},
		{"invoice_date", got.InvoiceDate == t.InvoiceDate},
		{"due_date", got.DueDate == t.DueDate},
		{"currency", got.Currency == t.Currency},
		{"total", errT == nil && got.Total == totalWant},
		{"vat_amount", errV == nil && got.VATAmount == vatWant},
	}
	var s FieldScore
	for _, c := range checks {
		s.Fields++
		if c.ok {
			s.Right++
		} else {
			s.Wrong = append(s.Wrong, c.name)
		}
	}
	return s
}
