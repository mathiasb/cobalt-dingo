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
	"math/big"
	"regexp"
	"strings"

	"github.com/mathiasb/cobalt-dingo/internal/extract"
	"github.com/mathiasb/cobalt-dingo/internal/fx"
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

	// ErrInvalidInvoice is returned for a date or currency that is not in the
	// expected form: both end up in the CSV, and both come from a model.
	ErrInvalidInvoice = errors.New("no entry: the invoice date or currency is not in a usable form")
)

var (
	dateRe     = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	currencyRe = regexp.MustCompile(`^[A-Z]{3}$`)
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

	// Original is set when the entry was converted to SEK: the foreign amounts and
	// the rate it was converted with, kept alongside so the conversion can be audited.
	Original *Original
}

// Original is the foreign-currency side of an entry that was converted to SEK.
type Original struct {
	Currency string
	// Lines are the foreign amounts in the same order as the entry's lines. A line
	// the conversion added (rounding) has no foreign amount.
	Lines []Line
	Rate  fx.Rate
}

// check is the last guard, independent of how the lines were derived: every
// line is a debit or a credit and neither is zero or negative, and the entry
// balances. Sums use big integers so that a wrap cannot hide in the check.
func (e Entry) check() error {
	d, c := new(big.Int), new(big.Int)
	for _, l := range e.Lines {
		if (l.Debit > 0) == (l.Credit > 0) || l.Debit < 0 || l.Credit < 0 {
			return fmt.Errorf("%w: line on account %d is not a single positive debit or credit", ErrUnbalanced, l.Account)
		}
		d.Add(d, big.NewInt(l.Debit))
		c.Add(c, big.NewInt(l.Credit))
	}
	if d.Cmp(c) != 0 {
		return ErrUnbalanced
	}
	return nil
}

// Balanced reports whether debits equal credits.
func (e Entry) Balanced() bool {
	d, c := new(big.Int), new(big.Int)
	for _, l := range e.Lines {
		d.Add(d, big.NewInt(l.Debit))
		c.Add(c, big.NewInt(l.Credit))
	}
	return d.Cmp(c) == 0
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
	case !dateRe.MatchString(inv.InvoiceDate) || !currencyRe.MatchString(inv.Currency):
		return Entry{}, ErrInvalidInvoice
	// Bounds first: every sum below is then far from int64's edge, so none can
	// wrap into a gap that looks like rounding.
	case inv.Total > extract.MaxAmountMinor || inv.NetTotal > extract.MaxAmountMinor || inv.VATAmount > extract.MaxAmountMinor:
		return Entry{}, fmt.Errorf("%w: an amount is larger than %d minor units", ErrUnbalanced, extract.MaxAmountMinor)
	case inv.Total == 0 || inv.NetTotal == 0:
		return Entry{}, fmt.Errorf("%w: the net or the total is zero", ErrUnbalanced)
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
		if inv.VATAmount == 0 {
			e.Warnings = append(e.Warnings, "reverse-charge VAT is not modelled: a foreign purchase of services normally needs input and output VAT (2645 and 2614), which this entry leaves out")
		}
	}
	if err := e.check(); err != nil {
		return Entry{}, err
	}
	return e, nil
}

// ToSEK converts a foreign-currency entry to SEK with the given rate. Each line
// is converted on its own, exactly, rounded half away from zero; the small
// difference that creates (at most one minor unit per line) goes to 3740 and
// larger is an error, never silently absorbed. The entry keeps the foreign
// amounts and the rate, and the supplier ledger is credited the converted total.
func (e Entry) ToSEK(r fx.Rate) (Entry, error) {
	switch {
	case e.Original != nil:
		return Entry{}, errors.New("the entry is already converted")
	case e.Currency == "SEK":
		return Entry{}, errors.New("a SEK entry has nothing to convert")
	case r.Currency != e.Currency:
		return Entry{}, fmt.Errorf("the %s rate cannot convert a %s entry", r.Currency, e.Currency)
	}
	out := Entry{Date: e.Date, Text: e.Text, Currency: "SEK"}
	orig := &Original{Currency: e.Currency, Rate: r}
	d, c := new(big.Int), new(big.Int)
	for _, l := range e.Lines {
		cl := Line{Account: l.Account}
		var err error
		if l.Debit > 0 {
			if cl.Debit, err = r.Convert(l.Debit); err != nil {
				return Entry{}, fmt.Errorf("%w: %v", ErrUnbalanced, err)
			}
		} else if cl.Credit, err = r.Convert(l.Credit); err != nil {
			return Entry{}, fmt.Errorf("%w: %v", ErrUnbalanced, err)
		}
		d.Add(d, big.NewInt(cl.Debit))
		c.Add(c, big.NewInt(cl.Credit))
		out.Lines = append(out.Lines, cl)
		orig.Lines = append(orig.Lines, l)
	}
	if diff := new(big.Int).Sub(d, c); diff.Sign() != 0 {
		abs := new(big.Int).Abs(diff)
		if abs.Cmp(big.NewInt(int64(len(e.Lines)))) > 0 {
			return Entry{}, fmt.Errorf("%w: converting the lines left a difference of %s minor units", ErrUnbalanced, abs)
		}
		add := Line{Account: roundingAccount}
		if diff.Sign() > 0 {
			add.Credit = abs.Int64()
		} else {
			add.Debit = abs.Int64()
		}
		out.Lines = append(out.Lines, add)
		orig.Lines = append(orig.Lines, Line{Account: roundingAccount})
	}
	for _, w := range e.Warnings {
		if strings.HasPrefix(w, "amounts are in ") && strings.HasSuffix(w, "not converted to SEK") {
			continue
		}
		out.Warnings = append(out.Warnings, w)
	}
	out.Warnings = append(out.Warnings, fmt.Sprintf(
		"converted from %s to SEK at %s (%s, rate of %s); indicative, not necessarily the rate your accountant uses",
		e.Currency, r.String(), r.Source, r.Date))
	out.Original = orig
	if err := out.check(); err != nil {
		return Entry{}, err
	}
	return out, nil
}

// WriteCSV writes entries, one row per line, numbered from 1 in the order given.
func WriteCSV(w io.Writer, entries []Entry) error {
	cw := csv.NewWriter(w)
	// The foreign-currency columns exist only when at least one entry was converted,
	// so a file of SEK entries is exactly what it was before conversion existed.
	converted := false
	for _, e := range entries {
		converted = converted || e.Original != nil
	}
	header := []string{"entry", "date", "text", "account", "debit", "credit", "currency", "warnings"}
	if converted {
		header = append(header, "original_currency", "original_debit", "original_credit", "rate", "rate_date", "rate_source")
	}
	if err := cw.Write(header); err != nil {
		return fmt.Errorf("write csv header: %w", err)
	}
	for i, e := range entries {
		warn := safe(strings.Join(e.Warnings, "; "))
		for j, l := range e.Lines {
			row := []string{
				fmt.Sprint(i + 1), safe(e.Date), safe(e.Text), fmt.Sprint(l.Account),
				amount(l.Debit), amount(l.Credit), safe(e.Currency), warn,
			}
			if converted {
				row = append(row, foreignCells(e, j)...)
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

// foreignCells are the six foreign-currency cells of line j, empty for an entry
// that was not converted and for a line the conversion added.
func foreignCells(e Entry, j int) []string {
	if e.Original == nil || j >= len(e.Original.Lines) {
		return make([]string, 6)
	}
	o := e.Original
	return []string{
		safe(o.Currency), amount(o.Lines[j].Debit), amount(o.Lines[j].Credit),
		safe(o.Rate.String()), safe(o.Rate.Date), safe(o.Rate.Source),
	}
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
