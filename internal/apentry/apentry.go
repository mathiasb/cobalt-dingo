// Package apentry turns a read invoice and its proposed account into the
// accounts payable journal entry it would become, and writes entries as CSV.
//
// It builds proposals. It never posts anything: the entry is data, and the CSV
// is a file the user chooses what to do with.
package apentry

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/mathiasb/cobalt-dingo/internal/process"
)

const (
	supplierLedger    = 2440 // Leverantörsskulder
	defaultInputVAT   = 2641 // Debiterad ingående moms
	roundingAccount   = 3740 // Öres- och kronutjämning
	roundingTolerance = 50   // minor units: öresavrundning is at most 50 öre
)

// Errors that explain why no entry was built. The page shows their text.
var (
	ErrDoNotTrust = errors.New("no entry: the amounts on this invoice do not add up, so nothing is proposed")
	ErrNoProposal = errors.New("no entry: no expense account could be proposed for this supplier")
	ErrCreditNote = errors.New("no entry: credit notes are not supported")
	ErrUnbalanced = errors.New("no entry: the amounts cannot be balanced")
)

// Line is one debit or credit. Amounts are minor units of the entry's currency.
type Line struct {
	Account       int
	Debit, Credit int64
}

// Entry is one proposed supplier-invoice journal entry.
type Entry struct {
	Date     string // YYYY-MM-DD
	Text     string
	Currency string
	Lines    []Line
	Warnings []string
}

// Balanced reports whether debits equal credits.
func (e Entry) Balanced() bool {
	var d, c int64
	for _, l := range e.Lines {
		d += l.Debit
		c += l.Credit
	}
	return d == c
}

// Build proposes the entry: debit the expense account for the net, debit input
// VAT when there is any, credit the supplier ledger for the total. A gap of up
// to 0.50 between net plus VAT and the total is Swedish öresavrundning and goes
// to 3740; a larger gap builds nothing. Amounts stay in the invoice currency.
func Build(res process.Result) (Entry, error) {
	inv := res.Invoice
	switch {
	case res.DoNotTrust:
		return Entry{}, ErrDoNotTrust
	case len(res.Proposal.Candidates) == 0:
		return Entry{}, ErrNoProposal
	case inv.Total < 0 || inv.NetTotal < 0 || inv.VATAmount < 0:
		return Entry{}, ErrCreditNote
	case !inv.NetKnown:
		return Entry{}, fmt.Errorf("%w: the net total was not read", ErrUnbalanced)
	}

	lines := []Line{{Account: res.Proposal.Candidates[0].Account, Debit: inv.NetTotal}}
	if inv.VATAmount > 0 {
		vat := res.Proposal.VATAccount
		if vat == 0 {
			vat = defaultInputVAT
		}
		lines = append(lines, Line{Account: vat, Debit: inv.VATAmount})
	}

	gap := inv.Total - (inv.NetTotal + inv.VATAmount)
	switch {
	case gap > roundingTolerance || gap < -roundingTolerance:
		return Entry{}, fmt.Errorf("%w: net plus VAT differs from the total by %d minor units", ErrUnbalanced, gap)
	case gap > 0:
		lines = append(lines, Line{Account: roundingAccount, Debit: gap})
	}
	lines = append(lines, Line{Account: supplierLedger, Credit: inv.Total})
	if gap < 0 {
		lines = append(lines, Line{Account: roundingAccount, Credit: -gap})
	}

	e := Entry{
		Date:     inv.InvoiceDate,
		Text:     fmt.Sprintf("%s faktura %s", inv.Supplier, inv.InvoiceNumber),
		Currency: inv.Currency,
		Lines:    lines,
	}
	if inv.InvoiceDateAmbiguous {
		e.Warnings = append(e.Warnings, fmt.Sprintf("invoice date could be read two ways; read as %s", inv.InvoiceDate))
	}
	if inv.Currency != "SEK" {
		e.Warnings = append(e.Warnings, fmt.Sprintf("amounts are in %s and not converted to SEK", inv.Currency))
	}
	if !e.Balanced() {
		return Entry{}, ErrUnbalanced
	}
	return e, nil
}

// WriteCSV writes entries, one row per line, numbered from 1 in the order given.
func WriteCSV(w io.Writer, entries []Entry) error {
	cw := csv.NewWriter(w)
	if err := cw.Write([]string{"entry", "date", "text", "account", "debit", "credit", "currency", "warnings"}); err != nil {
		return fmt.Errorf("write csv header: %w", err)
	}
	for i, e := range entries {
		warn := safe(strings.Join(e.Warnings, "; "))
		for _, l := range e.Lines {
			row := []string{
				fmt.Sprint(i + 1), e.Date, safe(e.Text), fmt.Sprint(l.Account),
				amount(l.Debit), amount(l.Credit), safe(e.Currency), warn,
			}
			if err := cw.Write(row); err != nil {
				return fmt.Errorf("write csv row: %w", err)
			}
		}
	}
	cw.Flush()
	if err := cw.Error(); err != nil {
		return fmt.Errorf("flush csv: %w", err)
	}
	return nil
}

func amount(minor int64) string {
	if minor == 0 {
		return ""
	}
	return fmt.Sprintf("%d.%02d", minor/100, minor%100)
}

// safe defuses a cell that a spreadsheet would run as a formula. The text comes
// from a model reading an uploaded image, so it is untrusted.
func safe(s string) string {
	if s != "" && strings.ContainsRune("=+-@\t\r", rune(s[0])) {
		return "'" + s
	}
	return s
}
