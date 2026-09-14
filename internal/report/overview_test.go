package report_test

import (
	"strings"
	"testing"
	"time"

	"github.com/mathiasb/cobalt-dingo/internal/domain"
	"github.com/mathiasb/cobalt-dingo/internal/report"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func sek(a float64) domain.Money { return domain.MoneyFromFloat(a, "SEK") }

func overview() domain.FinancialOverview {
	return domain.FinancialOverview{
		Year:      2,
		Currency:  "SEK",
		AsOf:      time.Date(2026, 9, 14, 20, 30, 0, 0, time.UTC),
		Freshness: domain.CacheFresh,
		Positions: []domain.AccountPosition{
			{Number: 1930, Description: "Företagskonto", Opening: sek(435527.94), Debit: sek(100000), Credit: sek(75), Closing: sek(535452.94)},
			{Number: 3011, Description: "Konsultarvoden", Credit: sek(100000), Closing: sek(-100000)},
		},
		VoucherCount:         405,
		Assets:               sek(535452.94),
		EquityAndLiabilities: sek(435527.94),
		Revenue:              sek(100000),
		Costs:                sek(75),
		Result:               sek(99925),
		Receivables:          sek(22500),
		Payables:             sek(4000),
	}
}

// ADR-0006's kill condition, stated as a test: "if any report presents a
// cached figure without its as-of date, the design has failed in practice."
// Every rendering carries it, in every freshness state — a conditional as-of
// line is the same failure with extra steps.
func TestRender_alwaysCarriesTheAsOfDate(t *testing.T) {
	for _, f := range []domain.CacheFreshness{domain.CacheFresh, domain.CacheUnverified, domain.CacheStale} {
		ov := overview()
		ov.Freshness = f
		out := report.Render(ov)
		assert.Contains(t, out, "2026-09-14", "freshness %q dropped the as-of date", f)
	}
}

// Unverified means completeness could not be checked. A reader skimming for
// the result must not have to infer that from a footnote.
func TestRender_unverifiedIsStatedWithItsReason(t *testing.T) {
	ov := overview()
	ov.Freshness = domain.CacheUnverified
	ov.Reason = "Fortnox unreachable: 503"

	out := report.Render(ov)
	assert.Contains(t, out, "UNVERIFIED")
	assert.Contains(t, out, "Fortnox unreachable: 503")
}

func TestRender_freshSaysSoWithoutAWarning(t *testing.T) {
	out := report.Render(overview())
	assert.NotContains(t, out, "UNVERIFIED")
	assert.Contains(t, out, "verified complete")
}

// A discrepancy must be impossible to miss and must never be presented next to
// a claim that the books balance.
func TestRender_discrepancyIsLoud(t *testing.T) {
	ov := overview()
	ov.Discrepancy = sek(2)

	out := report.Render(ov)
	assert.Contains(t, out, "DOES NOT BALANCE")
	assert.Contains(t, out, "SEK 2.00")
	assert.NotContains(t, out, "Balances: yes")
}

func TestRender_namesUnbalancedVouchers(t *testing.T) {
	ov := overview()
	ov.UnbalancedVouchers = []domain.UnbalancedVoucher{{Series: "A", Number: 17, Difference: sek(100)}}

	out := report.Render(ov)
	assert.Contains(t, out, "A/17")
	assert.Contains(t, out, "SEK 100.00")
}

// The voucher count is the evidence behind every derived figure. A report that
// omits how many entries it read cannot be sanity-checked at all.
func TestRender_statesHowManyVouchersItRead(t *testing.T) {
	assert.Contains(t, report.Render(overview()), "405")
}

func TestRender_showsPerAccountMovement(t *testing.T) {
	out := report.Render(overview())
	require.Contains(t, out, "1930")
	assert.Contains(t, out, "Företagskonto")
	assert.Contains(t, out, "535,452.94")
}

// Lines must be individually greppable: this output lands in a Job log, and
// the first thing anyone does with it is grep for an account number.
func TestRender_accountLinesAreOnePerLine(t *testing.T) {
	var found int
	for _, line := range strings.Split(report.Render(overview()), "\n") {
		if strings.Contains(line, "1930") || strings.Contains(line, "3011") {
			found++
		}
	}
	assert.Equal(t, 2, found)
}
