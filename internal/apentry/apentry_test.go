package apentry_test

import (
	"bytes"
	"encoding/csv"
	"math/big"
	"math/rand"
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

func TestBuild_RefusesAmountsThatWouldWrapAroundOrAreAbsurd(t *testing.T) {
	const huge = int64(9223372036854775807)
	tests := []struct {
		name            string
		net, vat, total int64
	}{
		{"net plus VAT wraps to a small gap", huge, huge, 0},
		{"net plus VAT wraps negative", huge, 1, huge},
		{"just over the largest amount accepted", extract.MaxAmountMinor + 1, 0, extract.MaxAmountMinor + 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, err := apentry.Build(read(tt.net, tt.vat, tt.total, "SEK"))

			require.Error(t, err)
			assert.Empty(t, e.Lines)
		})
	}
}

func TestBuild_AcceptsTheLargestAmountAllowed(t *testing.T) {
	_, err := apentry.Build(read(extract.MaxAmountMinor, 0, extract.MaxAmountMinor, "SEK"))

	require.NoError(t, err)
}

func TestBuild_RefusesAZeroTotalAndAZeroNet(t *testing.T) {
	for name, res := range map[string]process.Result{
		"everything zero":               read(0, 0, 0, "SEK"),
		"net zero with VAT":             read(0, 100, 100, "SEK"),
		"net and total with zero VAT":   read(0, 0, 0, "EUR"),
		"positive net and a zero total": read(100, 0, 0, "SEK"),
	} {
		t.Run(name, func(t *testing.T) {
			e, err := apentry.Build(res)

			require.Error(t, err)
			assert.Empty(t, e.Lines)
		})
	}
}

func TestBuild_ANegativeNetAloneOrANegativeVATAloneIsRefused(t *testing.T) {
	_, errNet := apentry.Build(read(-100, 2500, 2400, "SEK"))
	_, errVAT := apentry.Build(read(10000, -100, 9900, "SEK"))

	require.ErrorIs(t, errNet, apentry.ErrCreditNote)
	require.ErrorIs(t, errVAT, apentry.ErrCreditNote)
}

func TestBuild_RefusesAnInvoiceWhoseNetWasNotReadEvenIfTheCallerDidNotMarkItDoNotTrust(t *testing.T) {
	res := read(10000, 2500, 12500, "SEK")
	res.Invoice.NetKnown = false

	_, err := apentry.Build(res)

	require.ErrorIs(t, err, apentry.ErrUnbalanced)
}

func TestBuild_RefusesADateThatIsNotISOAndACurrencyThatIsNotThreeCapitalLetters(t *testing.T) {
	for name, mutate := range map[string]func(*process.Result){
		"empty date":          func(r *process.Result) { r.Invoice.InvoiceDate = "" },
		"formula as a date":   func(r *process.Result) { r.Invoice.InvoiceDate = "=1+1" },
		"date with a slash":   func(r *process.Result) { r.Invoice.InvoiceDate = "11/02/2026" },
		"empty currency":      func(r *process.Result) { r.Invoice.Currency = "" },
		"formula as currency": func(r *process.Result) { r.Invoice.Currency = "=1+" },
		"lower case":          func(r *process.Result) { r.Invoice.Currency = "sek" },
	} {
		t.Run(name, func(t *testing.T) {
			res := read(10000, 2500, 12500, "SEK")
			mutate(&res)

			_, err := apentry.Build(res)

			require.ErrorIs(t, err, apentry.ErrInvalidInvoice)
		})
	}
}

func TestBuild_AZeroVATForeignInvoiceWarnsThatReverseChargeVATIsNotModelled(t *testing.T) {
	foreign, err := apentry.Build(read(1750, 0, 1750, "EUR"))
	require.NoError(t, err)

	assert.Contains(t, strings.Join(foreign.Warnings, " | "), "reverse-charge VAT is not modelled")
}

// An independent check of the accounting invariant over many amounts: a built
// entry balances, no line is zero or negative, and each line is a debit or a
// credit, not both. The sums are done in big integers so a wrap in the code
// under test cannot also hide in the check.
func TestBuild_EveryEntryItBuildsBalancesAndHasNoZeroOrNegativeLine(t *testing.T) {
	rng := rand.New(rand.NewSource(20261125))
	pick := func() int64 {
		switch rng.Intn(6) {
		case 0:
			return 0
		case 1:
			return int64(rng.Intn(200)) - 100
		case 2:
			return extract.MaxAmountMinor - int64(rng.Intn(3))
		case 3:
			return 9223372036854775807 - int64(rng.Intn(3))
		default:
			return rng.Int63n(5_000_000)
		}
	}
	built := 0
	for i := 0; i < 20000; i++ {
		net, vat := pick(), pick()
		total := net + vat + int64(rng.Intn(121)) - 60 // may wrap; Build must cope
		e, err := apentry.Build(read(net, vat, total, "SEK"))
		if err != nil {
			continue
		}
		built++
		d, c := new(big.Int), new(big.Int)
		for _, l := range e.Lines {
			require.True(t, (l.Debit > 0) != (l.Credit > 0), "net=%d vat=%d total=%d line=%+v", net, vat, total, l)
			d.Add(d, big.NewInt(l.Debit))
			c.Add(c, big.NewInt(l.Credit))
		}
		require.Zero(t, d.Cmp(c), "net=%d vat=%d total=%d lines=%+v", net, vat, total, e.Lines)
	}
	require.Greater(t, built, 1000, "the property must be exercised on real entries")
}

func TestWriteCSV_DefusesTheDateCurrencyAndWarningCellsToo(t *testing.T) {
	e := apentry.Entry{
		Date: "=1+1", Text: "x", Currency: "=1+", Warnings: []string{`=cmd|' /C calc'!A0`},
		Lines: []apentry.Line{{Account: 6540, Debit: 100}, {Account: 2440, Credit: 100}},
	}

	rows := parse(t, []apentry.Entry{e})

	for _, r := range rows[1:] {
		for _, cell := range []string{r[1], r[2], r[6], r[7]} {
			assert.NotContains(t, "=+-@\t\r", cell[:1], "cell %q", cell)
		}
	}
}

func TestBalanced_DoesNotWrapAroundOnLargeAmounts(t *testing.T) {
	const huge = int64(9223372036854775807)
	wraps := apentry.Entry{Lines: []apentry.Line{{Account: 6540, Debit: huge}, {Account: 2641, Debit: huge}, {Account: 3740, Debit: 2}, {Account: 2440, Credit: 0}}}
	fine := apentry.Entry{Lines: []apentry.Line{{Account: 6540, Debit: huge}, {Account: 2440, Credit: huge}}}

	assert.False(t, wraps.Balanced())
	assert.True(t, fine.Balanced())
}
