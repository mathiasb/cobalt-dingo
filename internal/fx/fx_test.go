package fx_test

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mathiasb/cobalt-dingo/internal/fx"
)

const goodTable = `{
 "source": "Riksbanken SWEA daily rates (SEKxxxPMI)",
 "fetched": "2026-11-25",
 "currencies": {
  "EUR": [{"date":"2026-11-18","rate":"11.143"},{"date":"2026-11-19","rate":"11.147"},{"date":"2026-11-20","rate":"11.2125"},{"date":"2026-11-23","rate":"11.2375"},{"date":"2026-11-24","rate":"11.221"}],
  "USD": [{"date":"2026-11-20","rate":"10.20246"}]
 }
}`

func load(t *testing.T, doc string) *fx.Table {
	t.Helper()
	tb, err := fx.Load(strings.NewReader(doc))
	require.NoError(t, err)
	return tb
}

func TestLoad_ReadsRatesAsExactIntegersOfMicroSEKNotFloats(t *testing.T) {
	r, err := load(t, goodTable).Lookup("USD", "2026-11-20")

	require.NoError(t, err)
	assert.Equal(t, int64(10_202_460), r.Micro, "10.20246 SEK as micro-SEK")
	assert.Equal(t, "USD", r.Currency)
	assert.Equal(t, "2026-11-20", r.Date)
	assert.Equal(t, "Riksbanken SWEA daily rates (SEKxxxPMI)", r.Source)
}

func TestRate_StringShowsTheRateWithoutTrailingZerosButAtLeastFourDecimals(t *testing.T) {
	tb := load(t, goodTable)
	eur, _ := tb.Lookup("EUR", "2026-11-20")
	usd, _ := tb.Lookup("USD", "2026-11-20")
	eur2, _ := tb.Lookup("EUR", "2026-11-18")

	assert.Equal(t, "11.2125", eur.String())
	assert.Equal(t, "10.20246", usd.String())
	assert.Equal(t, "11.1430", eur2.String())
}

func TestLookup_UsesTheRateOfTheDayAndFallsBackToTheLatestEarlierDayWithinSevenDays(t *testing.T) {
	tb := load(t, goodTable)
	tests := []struct {
		name, date, wantRateDate string
	}{
		{"the day itself", "2026-11-23", "2026-11-23"},
		{"saturday uses friday", "2026-11-21", "2026-11-20"},
		{"sunday uses friday", "2026-11-22", "2026-11-20"},
		{"seven days after the last rate", "2026-12-01", "2026-11-24"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, err := tb.Lookup("EUR", tt.date)

			require.NoError(t, err)
			assert.Equal(t, tt.wantRateDate, r.Date)
		})
	}
}

func TestLookup_RefusesWhenThereIsNoRateAndSaysWhy(t *testing.T) {
	tb := load(t, goodTable)
	tests := []struct {
		name, cur, date, want string
	}{
		{"eight days after the last rate", "EUR", "2026-12-02", "EUR"},
		{"before the first rate", "EUR", "2026-11-17", "EUR"},
		{"a currency not in the table", "DKK", "2026-11-20", "DKK"},
		{"a date that is not a date", "EUR", "2026-13-40", "date"},
		{"a currency that is not a code", "eur", "2026-11-20", "currency"},
		{"an empty date", "EUR", "", "date"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tb.Lookup(tt.cur, tt.date)

			require.ErrorIs(t, err, fx.ErrNoRate)
			assert.Contains(t, err.Error(), tt.want)
		})
	}
}

func TestLoad_RefusesAFileThatIsMalformedOrHostile(t *testing.T) {
	obs := func(d, r string) string { return fmt.Sprintf(`{"date":%q,"rate":%q}`, d, r) }
	wrap := func(cur, body string) string {
		return `{"source":"s","fetched":"2026-11-25","currencies":{"` + cur + `":[` + body + `]}}`
	}
	many := make([]string, 0, fx.MaxObservations+1)
	for i := 0; i <= fx.MaxObservations; i++ {
		many = append(many, obs(fmt.Sprintf("%04d-%02d-%02d", 1000+i/372, (i/31)%12+1, i%31+1), "1.0"))
	}
	tests := []struct {
		name, doc, want string
	}{
		{"not json", `hello`, "json"},
		{"an unknown field", `{"source":"s","fetched":"2026-11-25","currencies":{},"extra":1}`, "extra"},
		{"no source", `{"source":"","fetched":"2026-11-25","currencies":{"EUR":[` + obs("2026-11-18", "11.1") + `]}}`, "source"},
		{"a source that is too long", `{"source":"` + strings.Repeat("x", 300) + `","fetched":"2026-11-25","currencies":{"EUR":[` + obs("2026-11-18", "11.1") + `]}}`, "source"},
		{"no currencies", `{"source":"s","fetched":"2026-11-25","currencies":{}}`, "no rates"},
		{"a currency that is not a code", wrap("euro", obs("2026-11-18", "11.1")), "currency"},
		{"SEK itself", wrap("SEK", obs("2026-11-18", "1.0")), "SEK"},
		{"a date that is not a date", wrap("EUR", obs("2026-02-30", "11.1")), "date"},
		{"a rate that is not a number", wrap("EUR", obs("2026-11-18", "abc")), "rate"},
		{"a rate with an exponent", wrap("EUR", obs("2026-11-18", "1e1")), "rate"},
		{"a negative rate", wrap("EUR", obs("2026-11-18", "-11.1")), "rate"},
		{"a zero rate", wrap("EUR", obs("2026-11-18", "0")), "rate"},
		{"a rate with seven decimals", wrap("EUR", obs("2026-11-18", "11.1234567")), "rate"},
		{"a rate with a comma", wrap("EUR", obs("2026-11-18", "11,1")), "rate"},
		{"a rate far too small", wrap("EUR", obs("2026-11-18", "0.00001")), "rate"},
		{"a rate far too large", wrap("EUR", obs("2026-11-18", "999999")), "rate"},
		{"a rate sent as a json number", wrap("EUR", `{"date":"2026-11-18","rate":11.1}`), "rate"},
		{"duplicate dates", wrap("EUR", obs("2026-11-18", "11.1")+","+obs("2026-11-18", "11.2")), "order"},
		{"dates out of order", wrap("EUR", obs("2026-11-19", "11.1")+","+obs("2026-11-18", "11.2")), "order"},
		{"too many observations", wrap("EUR", strings.Join(many, ",")), "too many"},
		{"a currency listed twice", `{"source":"s","fetched":"2026-11-25","currencies":{"EUR":[` + obs("2026-11-18", "11.1") + `],"EUR":[` + obs("2026-11-18", "99.9") + `]}}`, "duplicate"},
		{"the source given twice", `{"source":"s","source":"t","fetched":"2026-11-25","currencies":{"EUR":[` + obs("2026-11-18", "11.1") + `]}}`, "duplicate"},
		{"a duplicate key inside an observation", `{"source":"s","fetched":"2026-11-25","currencies":{"EUR":[{"date":"2026-11-18","rate":"11.1","rate":"99.9"}]}}`, "duplicate"},
		{"data after the table", `{"source":"s","fetched":"2026-11-25","currencies":{"EUR":[` + obs("2026-11-18", "11.1") + `]}} {"more":1}`, "after"},
		{"only empty lists", `{"source":"s","fetched":"2026-11-25","currencies":{"EUR":[],"USD":[]}}`, "no rates"},
		{"larger than the size limit", `{"source":"` + strings.Repeat("x", fx.MaxFileBytes) + `"}`, "larger"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tb, err := fx.Load(strings.NewReader(tt.doc))

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.want)
			assert.Nil(t, tb, "no half-loaded table")
		})
	}
}

func TestConvert_IsExactIntegerArithmeticRoundedHalfAwayFromZero(t *testing.T) {
	r, err := load(t, goodTable).Lookup("EUR", "2026-11-20") // 11.2125
	require.NoError(t, err)
	tests := []struct {
		name  string
		minor int64
		want  int64
	}{
		{"one euro", 100, 1121},                   // 1121.25 -> 1121
		{"a hundred euro", 10000, 112125},         // exact
		{"one cent", 1, 11},                       // 11.2125 -> 11
		{"a half rounds away from zero", 40, 449}, // 448.5 -> 449
		{"zero", 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := r.Convert(tt.minor)

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestConvert_RefusesANegativeAmountAndAnOverflowInsteadOfWrapping(t *testing.T) {
	tb := load(t, `{"source":"s","fetched":"2026-11-25","currencies":{"XYZ":[{"date":"2026-11-18","rate":"100000"}]}}`)
	r, err := tb.Lookup("XYZ", "2026-11-18")
	require.NoError(t, err)

	_, err = r.Convert(-1)
	require.Error(t, err, "a negative amount is a credit note, which is not converted here")

	got, err := r.Convert(1_000_000_000_000) // the largest amount accepted, at the largest rate accepted
	require.NoError(t, err)
	assert.Equal(t, int64(100_000_000_000_000_000), got)

	_, err = r.Convert(1 << 62)
	require.Error(t, err, "too large to convert exactly")
}

// Whatever rate and amount: converting never panics, never goes negative, and
// monotonic amounts stay monotonic.
func TestConvert_NeverNegativeAndNeverDecreasesWhenTheAmountGrows(t *testing.T) {
	tb := load(t, goodTable)
	for _, cur := range []string{"EUR", "USD"} {
		r, err := tb.Lookup(cur, "2026-11-20")
		require.NoError(t, err)
		var prev int64
		for m := int64(0); m < 5000; m += 7 {
			got, err := r.Convert(m)
			require.NoError(t, err)
			require.GreaterOrEqual(t, got, prev, "%s %d", cur, m)
			prev = got
		}
	}
}

// The file scripts/fetch-riksbank-rates.py writes, a sample of real published
// rates, loads as it is. A weekend date uses the Friday rate.
func TestLoad_TheShapeTheFetchScriptWritesLoadsAndAWeekendUsesFriday(t *testing.T) {
	f, err := os.Open("testdata/riksbank-sample.json")
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	tb, err := fx.Load(f)

	require.NoError(t, err)
	assert.Equal(t, []string{"CHF", "DKK", "EUR", "GBP", "NOK", "USD"}, tb.Currencies())
	sat, err := tb.Lookup("EUR", "2026-11-21")
	require.NoError(t, err)
	assert.Equal(t, "2026-11-20", sat.Date)
	assert.Equal(t, "11.2125", sat.String())
	assert.Contains(t, sat.Source, "Riksbanken")
}

// The amount cap is a rule of its own, not only a side effect of overflow: one
// minor unit over it is refused even at a rate of one.
func TestConvert_RefusesAnAmountOneAboveTheCapEvenAtAUnitRate(t *testing.T) {
	tb := load(t, `{"source":"s","fetched":"2026-11-25","currencies":{"XYZ":[{"date":"2026-11-18","rate":"1"}]}}`)
	r, err := tb.Lookup("XYZ", "2026-11-18")
	require.NoError(t, err)

	got, err := r.Convert(1_000_000_000_000)
	require.NoError(t, err)
	assert.Equal(t, int64(1_000_000_000_000), got)

	_, err = r.Convert(1_000_000_000_001)
	require.Error(t, err)
}

// Rates normally arrive through Lookup, which has validated them. A Rate built by
// hand is held to the same range, so the converter cannot be used with a rate the
// loader would have refused.
func TestConvert_RefusesARateOutsideTheAcceptedRangeEvenWhenBuiltByHand(t *testing.T) {
	for _, micro := range []int64{0, 1, 99, 100_000_000_001, -5} {
		_, err := fx.Rate{Currency: "EUR", Micro: micro}.Convert(100)

		require.Error(t, err, "micro %d", micro)
	}
	_, err := fx.Rate{Currency: "EUR", Micro: 100}.Convert(100)
	require.NoError(t, err, "the lowest accepted rate converts")
	_, err = fx.Rate{Currency: "EUR", Micro: 100_000_000_000}.Convert(100)
	require.NoError(t, err, "the highest accepted rate converts")
}

// Every currency the acceptance criteria name, from real published rates: NOK is
// quoted per one krone and DKK per one krone, so neither needs a per-100 factor.
func TestLoad_TheSampleCoversEveryCurrencyTheDemoConverts(t *testing.T) {
	f, err := os.Open("testdata/riksbank-sample.json")
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	tb, err := fx.Load(f)
	require.NoError(t, err)
	assert.Equal(t, []string{"CHF", "DKK", "EUR", "GBP", "NOK", "USD"}, tb.Currencies())

	for _, cur := range tb.Currencies() {
		r, err := tb.Lookup(cur, "2026-11-20")
		require.NoError(t, err, cur)
		got, err := r.Convert(10000) // 100.00 in the currency
		require.NoError(t, err, cur)
		// A rate per ONE unit puts 100 units between 5 SEK (JPY-like) and 2000 SEK.
		assert.Greater(t, got, int64(10000), "%s: 100 units should be worth more than 100 SEK-minor, got %d", cur, got)
		assert.Less(t, got, int64(2_500_000), "%s: 100 units should be worth less than 2500 SEK, got %d", cur, got)
	}
}
