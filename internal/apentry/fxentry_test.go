package apentry_test

import (
	"fmt"
	"math/big"
	"math/rand"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mathiasb/cobalt-dingo/internal/apentry"
	"github.com/mathiasb/cobalt-dingo/internal/extract"
	"github.com/mathiasb/cobalt-dingo/internal/fx"
)

// rateFor returns the rate for one currency on 2026-11-02 (the day of read()'s invoice).
func rateFor(t *testing.T, cur, rate, source string) fx.Rate {
	t.Helper()
	doc := fmt.Sprintf(`{"source":%q,"fetched":"2026-11-25","currencies":{%q:[{"date":"2026-11-02","rate":%q}]}}`, source, cur, rate)
	tb, err := fx.Load(strings.NewReader(doc))
	require.NoError(t, err)
	r, err := tb.Lookup(cur, "2026-11-02")
	require.NoError(t, err)
	return r
}

func eur(t *testing.T) fx.Rate { return rateFor(t, "EUR", "11.2125", "Riksbanken test rates") }

func TestToSEK_PostsInSEKAndKeepsTheForeignAmountsAndTheRateAlongside(t *testing.T) {
	e, err := apentry.Build(read(10000, 0, 10000, "EUR"))
	require.NoError(t, err)

	s, err := e.ToSEK(eur(t))

	require.NoError(t, err)
	assert.Equal(t, "SEK", s.Currency)
	assert.Equal(t, []line{{6540, 112125, 0}, {2440, 0, 112125}}, lines(s))
	assert.True(t, s.Balanced())
	require.NotNil(t, s.Original)
	assert.Equal(t, "EUR", s.Original.Currency)
	assert.Equal(t, []line{{6540, 10000, 0}, {2440, 0, 10000}}, func() []line {
		var out []line
		for _, l := range s.Original.Lines {
			out = append(out, line{l.Account, l.Debit, l.Credit})
		}
		return out
	}())
	assert.Equal(t, "11.2125", s.Original.Rate.String())
	assert.Equal(t, "2026-11-02", s.Original.Rate.Date)
	assert.Equal(t, "Riksbanken test rates", s.Original.Rate.Source)
}

func TestToSEK_ReplacesTheNotConvertedWarningWithTheRateAndKeepsTheReverseChargeOne(t *testing.T) {
	e, err := apentry.Build(read(10000, 0, 10000, "EUR"))
	require.NoError(t, err)

	s, err := e.ToSEK(eur(t))

	require.NoError(t, err)
	joined := strings.Join(s.Warnings, " | ")
	assert.NotContains(t, joined, "not converted to SEK")
	assert.Contains(t, joined, "converted from EUR to SEK at 11.2125")
	assert.Contains(t, joined, "indicative")
	assert.Contains(t, joined, "reverse-charge VAT is not modelled", "currency conversion does not touch the VAT rules")
}

// Each line is converted on its own and the entry must still balance: a rounding
// difference of a minor unit goes to 3740, as the foreign-currency gap does. Both
// directions are exercised: the converted parts can add to more or to less than
// the converted total.
func TestToSEK_RoundingFromConvertingLinesSeparatelyLandsOn3740AndTheEntryBalancesInBothDirections(t *testing.T) {
	r := eur(t)
	find := func(want func(parts, whole int64) bool) (int64, int64) {
		for n := int64(100); n < 600; n++ {
			for v := int64(1); v < 200; v++ {
				a, _ := r.Convert(n)
				b, _ := r.Convert(v)
				c, _ := r.Convert(n + v)
				if want(a+b, c) {
					return n, v
				}
			}
		}
		return 0, 0
	}
	for name, tc := range map[string]struct {
		pick       func(parts, whole int64) bool
		onCredit   bool // the rounding line is a credit when the converted debits exceed the credit
		wantSideOf string
	}{
		"parts add to more than the total": {func(p, w int64) bool { return p > w }, true, "credit"},
		"parts add to less than the total": {func(p, w int64) bool { return p < w }, false, "debit"},
	} {
		t.Run(name, func(t *testing.T) {
			net, vat := find(tc.pick)
			require.NotZero(t, net, "the search must find amounts of this kind")
			e, err := apentry.Build(read(net, vat, net+vat, "EUR"))
			require.NoError(t, err)

			s, err := e.ToSEK(r)

			require.NoError(t, err)
			assert.True(t, s.Balanced())
			total, _ := r.Convert(net + vat)
			var credit2440, rDebit, rCredit int64
			for _, l := range s.Lines {
				if l.Account == 2440 {
					credit2440 += l.Credit
				}
				if l.Account == 3740 {
					rDebit += l.Debit
					rCredit += l.Credit
				}
			}
			assert.Equal(t, total, credit2440, "the supplier ledger is credited the converted total, whatever the parts convert to")
			if tc.onCredit {
				assert.Equal(t, int64(1), rCredit, "debits exceeded credits, so 3740 takes the difference as a credit")
				assert.Zero(t, rDebit)
			} else {
				assert.Equal(t, int64(1), rDebit, "credits exceeded debits, so 3740 takes the difference as a debit")
				assert.Zero(t, rCredit)
			}
			assert.LessOrEqual(t, rDebit+rCredit, int64(len(e.Lines)))
		})
	}
}

// An entry that was already out of balance in the foreign currency must not have
// its difference quietly put on 3740 by the conversion.
func TestToSEK_RefusesAnEntryThatWasNotBalancedInTheForeignCurrencyInsteadOfAbsorbingTheGap(t *testing.T) {
	e := apentry.Entry{Date: "2026-11-02", Text: "x", Currency: "EUR", Lines: []apentry.Line{
		{Account: 6540, Debit: 100_000}, {Account: 2440, Credit: 50_000},
	}}

	_, err := e.ToSEK(eur(t))

	require.ErrorIs(t, err, apentry.ErrUnbalanced)
}

func TestToSEK_AnEntryThatCarriesAnOriginalIsNotConvertedAgain(t *testing.T) {
	e := apentry.Entry{Date: "2026-11-02", Text: "x", Currency: "EUR", Original: &apentry.Original{Currency: "EUR"}, Lines: []apentry.Line{
		{Account: 6540, Debit: 100}, {Account: 2440, Credit: 100},
	}}

	_, err := e.ToSEK(eur(t))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "already converted")
}

func TestToSEK_AnExistingForeignRoundingLineIsConvertedLikeTheRest(t *testing.T) {
	e, err := apentry.Build(read(1000, 0, 1020, "EUR")) // 20 minor units of öresavrundning
	require.NoError(t, err)

	s, err := e.ToSEK(eur(t))

	require.NoError(t, err)
	assert.True(t, s.Balanced())
	var rounding int64
	for _, l := range s.Lines {
		if l.Account == 3740 {
			rounding += l.Debit
		}
	}
	assert.Equal(t, int64(224), rounding, "20 × 11.2125 = 224.25")
}

func TestToSEK_RefusesWhatIsNotAForeignEntryOrTheWrongRate(t *testing.T) {
	foreign, err := apentry.Build(read(10000, 0, 10000, "EUR"))
	require.NoError(t, err)
	domestic, err := apentry.Build(read(10000, 2500, 12500, "SEK"))
	require.NoError(t, err)

	_, err = domestic.ToSEK(eur(t))
	assert.Error(t, err, "a SEK entry has nothing to convert")

	_, err = foreign.ToSEK(rateFor(t, "USD", "10.2", "s"))
	assert.Error(t, err, "a USD rate cannot convert a EUR entry")

	once, err := foreign.ToSEK(eur(t))
	require.NoError(t, err)
	_, err = once.ToSEK(eur(t))
	assert.Error(t, err, "an entry is converted once")
}

func TestToSEK_AnAmountThatConvertsToZeroBuildsNoEntryInsteadOfAZeroLine(t *testing.T) {
	e, err := apentry.Build(read(1, 0, 1, "XYZ"))
	require.NoError(t, err)

	_, err = e.ToSEK(rateFor(t, "XYZ", "0.0001", "s"))

	require.ErrorIs(t, err, apentry.ErrUnbalanced)
}

func TestToSEK_TheLargestAmountAtASizeableRateConvertsExactly(t *testing.T) {
	e, err := apentry.Build(read(extract.MaxAmountMinor, 0, extract.MaxAmountMinor, "USD"))
	require.NoError(t, err)

	s, err := e.ToSEK(rateFor(t, "USD", "10.20246", "s"))

	require.NoError(t, err)
	assert.True(t, s.Balanced())
	assert.Equal(t, int64(10_202_460_000_000), s.Lines[0].Debit, "1e12 minor × 10.20246 = 10202460000000")
}

// The invariant over many rates and amounts, checked with big integers apart
// from the code under test: a converted entry balances, no line is zero or
// negative, and the supplier ledger is credited exactly the converted total.
func TestToSEK_EveryConvertedEntryBalancesCreditsTheConvertedTotalAndHasNoZeroLine(t *testing.T) {
	rng := rand.New(rand.NewSource(20261125))
	converted := 0
	for i := 0; i < 20000; i++ {
		micro := int64(100 + rng.Intn(1_000_000_000))
		if rng.Intn(4) == 0 {
			micro = int64(100 + rng.Intn(100_000))
		}
		rate := rateFor(t, "EUR", fmt.Sprintf("%d.%06d", micro/1_000_000, micro%1_000_000), "s")
		net := rng.Int63n(100_000_000) + 1
		vat := rng.Int63n(30_000_000)
		gap := int64(rng.Intn(101)) - 50
		total := net + vat + gap
		e, err := apentry.Build(read(net, vat, total, "EUR"))
		if err != nil {
			continue
		}
		s, err := e.ToSEK(rate)
		if err != nil {
			continue
		}
		converted++
		d, c := new(big.Int), new(big.Int)
		var ledger int64
		for _, l := range s.Lines {
			require.True(t, (l.Debit > 0) != (l.Credit > 0), "rate=%d net=%d vat=%d total=%d line=%+v", micro, net, vat, total, l)
			d.Add(d, big.NewInt(l.Debit))
			c.Add(c, big.NewInt(l.Credit))
			if l.Account == 2440 {
				ledger += l.Credit
			}
		}
		require.Zero(t, d.Cmp(c), "rate=%d net=%d vat=%d total=%d lines=%+v", micro, net, vat, total, s.Lines)
		want, err := rate.Convert(total)
		require.NoError(t, err)
		require.Equal(t, want, ledger, "rate=%d total=%d", micro, total)
	}
	require.Greater(t, converted, 10000, "the property must be exercised on real conversions")
}

func TestWriteCSV_AConvertedEntryCarriesTheForeignAmountsTheRateItsDateAndSource(t *testing.T) {
	e, err := apentry.Build(read(10000, 0, 10000, "EUR"))
	require.NoError(t, err)
	s, err := e.ToSEK(eur(t))
	require.NoError(t, err)

	rows := parse(t, []apentry.Entry{s})

	assert.Equal(t, []string{
		"entry", "date", "text", "account", "debit", "credit", "currency", "warnings",
		"original_currency", "original_debit", "original_credit", "rate", "rate_date", "rate_source",
	}, rows[0])
	assert.Equal(t, "6540", rows[1][3])
	assert.Equal(t, "1121.25", rows[1][4])
	assert.Equal(t, "SEK", rows[1][6])
	assert.Equal(t, []string{"EUR", "100.00", "", "11.2125", "2026-11-02", "Riksbanken test rates"}, rows[1][8:])
	assert.Equal(t, []string{"EUR", "", "100.00", "11.2125", "2026-11-02", "Riksbanken test rates"}, rows[2][8:])
}

func TestWriteCSV_ABatchWithNoConvertedEntryKeepsTheOriginalColumnsExactly(t *testing.T) {
	a, err := apentry.Build(read(10000, 2500, 12500, "SEK"))
	require.NoError(t, err)

	rows := parse(t, []apentry.Entry{a})

	assert.Equal(t, []string{"entry", "date", "text", "account", "debit", "credit", "currency", "warnings"}, rows[0])
	assert.Len(t, rows[1], 8)
}

func TestWriteCSV_AMixedBatchLeavesTheForeignColumnsEmptyOnSEKRows(t *testing.T) {
	a, err := apentry.Build(read(10000, 2500, 12500, "SEK"))
	require.NoError(t, err)
	f, err := apentry.Build(read(10000, 0, 10000, "EUR"))
	require.NoError(t, err)
	s, err := f.ToSEK(eur(t))
	require.NoError(t, err)

	rows := parse(t, []apentry.Entry{a, s})

	require.Len(t, rows[0], 14)
	assert.Equal(t, []string{"", "", "", "", "", ""}, rows[1][8:], "a SEK row has no foreign columns")
	assert.Equal(t, "EUR", rows[4][8])
}

func TestWriteCSV_ARateSourceThatLooksLikeAFormulaIsDefused(t *testing.T) {
	e, err := apentry.Build(read(10000, 0, 10000, "EUR"))
	require.NoError(t, err)
	s, err := e.ToSEK(rateFor(t, "EUR", "11.2125", `=HYPERLINK("http://evil","x")`))
	require.NoError(t, err)

	rows := parse(t, []apentry.Entry{s})

	assert.True(t, strings.HasPrefix(rows[1][13], "'="), "got %q", rows[1][13])
}
