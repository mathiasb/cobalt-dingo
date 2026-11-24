package apentry_test

import (
	"bytes"
	"encoding/csv"
	"strings"
	"testing"

	"github.com/mathiasb/cobalt-dingo/internal/apentry"
	"github.com/mathiasb/cobalt-dingo/internal/booking"
	"github.com/mathiasb/cobalt-dingo/internal/extract"
	"github.com/mathiasb/cobalt-dingo/internal/process"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// read builds the result of reading an invoice and proposing an account.
func read(net, vat, total int64, cur string) process.Result {
	return process.Result{
		Invoice: extract.Invoice{
			Supplier: "Hetzner Online GmbH", InvoiceNumber: "081000972341", InvoiceDate: "2026-11-02",
			Currency: cur, NetTotal: net, NetKnown: true, VATAmount: vat, Total: total,
		},
		Proposal: booking.Proposal{
			Candidates: []booking.Candidate{{Account: 6540, Score: 1}},
			Confidence: 1,
		},
	}
}

type line struct {
	Account       int
	Debit, Credit int64
}

func lines(e apentry.Entry) []line {
	var out []line
	for _, l := range e.Lines {
		out = append(out, line{l.Account, l.Debit, l.Credit})
	}
	return out
}

func TestBuild_ADomesticInvoiceDebitsExpenseAndInputVATAndCreditsTheSupplierLedger(t *testing.T) {
	e, err := apentry.Build(read(10000, 2500, 12500, "SEK"))

	require.NoError(t, err)
	assert.Equal(t, []line{{6540, 10000, 0}, {2641, 2500, 0}, {2440, 0, 12500}}, lines(e))
	assert.True(t, e.Balanced())
	assert.Equal(t, "2026-11-02", e.Date)
	assert.True(t, strings.HasPrefix(e.Text, "Hetzner Online GmbH"))
	assert.Contains(t, e.Text, "081000972341")
}

func TestBuild_UsesTheInputVATAccountThePastBookingsUsedElseTheDefault(t *testing.T) {
	withHistory := read(10000, 2500, 12500, "SEK")
	withHistory.Proposal.VATAccount = 2645

	a, err := apentry.Build(withHistory)
	require.NoError(t, err)
	b, err := apentry.Build(read(10000, 2500, 12500, "SEK"))
	require.NoError(t, err)

	assert.Equal(t, 2645, a.Lines[1].Account)
	assert.Equal(t, 2641, b.Lines[1].Account)
}

func TestBuild_AZeroVATInvoiceHasNoVATLine(t *testing.T) {
	e, err := apentry.Build(read(1750, 0, 1750, "EUR"))

	require.NoError(t, err)
	assert.Equal(t, []line{{6540, 1750, 0}, {2440, 0, 1750}}, lines(e))
	assert.True(t, e.Balanced())
}

func TestBuild_ARoundingGapWithinFiftyMinorUnitsIsBalancedOn3740(t *testing.T) {
	tests := []struct {
		name            string
		net, vat, total int64
		want            []line
	}{
		{
			"total above net plus VAT: the extra is a debit", 10000, 2480, 12500,
			[]line{{6540, 10000, 0}, {2641, 2480, 0}, {3740, 20, 0}, {2440, 0, 12500}},
		},
		{
			"total below net plus VAT: the shortfall is a credit", 10000, 2500, 12480,
			[]line{{6540, 10000, 0}, {2641, 2500, 0}, {2440, 0, 12480}, {3740, 0, 20}},
		},
		{
			"exactly fifty over is still rounding", 10000, 2500, 12550,
			[]line{{6540, 10000, 0}, {2641, 2500, 0}, {3740, 50, 0}, {2440, 0, 12550}},
		},
		{
			"exactly fifty under is still rounding", 10000, 2500, 12450,
			[]line{{6540, 10000, 0}, {2641, 2500, 0}, {2440, 0, 12450}, {3740, 0, 50}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, err := apentry.Build(read(tt.net, tt.vat, tt.total, "SEK"))

			require.NoError(t, err)
			assert.Equal(t, tt.want, lines(e))
			assert.True(t, e.Balanced())
		})
	}
}

func TestBuild_RefusesAGapBeyondTheToleranceEvenIfTheCallerDidNotMarkItDoNotTrust(t *testing.T) {
	res := read(10000, 2500, 12600, "SEK") // 100 over, DoNotTrust left false

	_, err := apentry.Build(res)

	require.ErrorIs(t, err, apentry.ErrUnbalanced)
}

func TestBuild_RefusesAGapOfFiftyOneInEitherDirection(t *testing.T) {
	for _, total := range []int64{12551, 12449} {
		_, err := apentry.Build(read(10000, 2500, total, "SEK"))

		require.ErrorIs(t, err, apentry.ErrUnbalanced, "total %d", total)
	}
}

func TestBuild_BuildsNoEntryForADoNotTrustResult(t *testing.T) {
	res := read(10000, 2500, 12500, "SEK")
	res.DoNotTrust = true

	_, err := apentry.Build(res)

	require.ErrorIs(t, err, apentry.ErrDoNotTrust)
}

func TestBuild_BuildsNoEntryWhenNoAccountWasProposed(t *testing.T) {
	res := read(10000, 2500, 12500, "SEK")
	res.Proposal = booking.Proposal{}

	_, err := apentry.Build(res)

	require.ErrorIs(t, err, apentry.ErrNoProposal)
}

func TestBuild_BuildsNoEntryForACreditNote(t *testing.T) {
	_, err := apentry.Build(read(-10000, -2500, -12500, "SEK"))

	require.ErrorIs(t, err, apentry.ErrCreditNote)
}

func TestBuild_CarriesWarningsForAnAmbiguousDateAndANonSEKCurrency(t *testing.T) {
	res := read(1750, 0, 1750, "EUR")
	res.Invoice.InvoiceDateAmbiguous = true

	e, err := apentry.Build(res)

	require.NoError(t, err)
	joined := strings.Join(e.Warnings, " | ")
	assert.Contains(t, joined, "could be read two ways")
	assert.Contains(t, joined, "not converted to SEK")
}

func TestBuild_ASEKInvoiceWithAClearDateHasNoWarnings(t *testing.T) {
	e, err := apentry.Build(read(10000, 2500, 12500, "SEK"))

	require.NoError(t, err)
	assert.Empty(t, e.Warnings)
}

func parse(t *testing.T, entries []apentry.Entry) [][]string {
	t.Helper()
	var buf bytes.Buffer
	require.NoError(t, apentry.WriteCSV(&buf, entries))
	rows, err := csv.NewReader(&buf).ReadAll()
	require.NoError(t, err)
	return rows
}

func TestWriteCSV_HasOneRowPerLineWithDecimalAmountsAndTheEntryNumber(t *testing.T) {
	e, err := apentry.Build(read(10000, 2500, 12500, "SEK"))
	require.NoError(t, err)

	rows := parse(t, []apentry.Entry{e, e})

	require.Len(t, rows, 1+3+3)
	assert.Equal(t, []string{"entry", "date", "text", "account", "debit", "credit", "currency", "warnings"}, rows[0])
	assert.Equal(t, []string{"1", "2026-11-02", e.Text, "6540", "100.00", "", "SEK", ""}, rows[1])
	assert.Equal(t, "2440", rows[3][3])
	assert.Equal(t, "125.00", rows[3][5])
	assert.Equal(t, "2", rows[4][0], "the second entry is numbered 2")
}

func TestWriteCSV_EveryEntryInTheFileBalances(t *testing.T) {
	a, _ := apentry.Build(read(10000, 2480, 12500, "SEK"))
	b, _ := apentry.Build(read(1750, 0, 1750, "EUR"))

	rows := parse(t, []apentry.Entry{a, b})

	debit, credit := map[string]float64{}, map[string]float64{}
	for _, r := range rows[1:] {
		debit[r[0]] += num(r[4])
		credit[r[0]] += num(r[5])
	}
	for k := range debit {
		assert.InDelta(t, debit[k], credit[k], 0.001, "entry %s", k)
	}
}

func num(s string) float64 {
	if s == "" {
		return 0
	}
	var whole, frac int64
	parts := strings.SplitN(s, ".", 2)
	for _, c := range parts[0] {
		whole = whole*10 + int64(c-'0')
	}
	for _, c := range parts[1] {
		frac = frac*10 + int64(c-'0')
	}
	return float64(whole) + float64(frac)/100
}

// The supplier text comes from a model reading an uploaded image, so it is
// untrusted. A cell that starts with one of these runs as a formula when the
// file is opened in a spreadsheet.
func TestWriteCSV_ATextThatStartsLikeAFormulaIsNotLeftExecutable(t *testing.T) {
	for _, bad := range []string{`=HYPERLINK("http://evil.example","x")`, "+1+1", "-2+3", "@SUM(A1)", "\t=1+1", "\r=1+1"} {
		t.Run(bad, func(t *testing.T) {
			res := read(10000, 2500, 12500, "SEK")
			res.Invoice.Supplier = bad
			e, err := apentry.Build(res)
			require.NoError(t, err)

			rows := parse(t, []apentry.Entry{e})

			for _, r := range rows[1:] {
				first := r[2][:1]
				assert.NotContains(t, "=+-@\t\r", first, "text cell %q", r[2])
			}
		})
	}
}
