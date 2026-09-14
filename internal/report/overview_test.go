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

// An unbalanced voucher prints the rows that were read. Without them the
// finding is a dead end: Fortnox will not accept an unbalanced voucher through
// its own UI, so the difference means either the API returned an incomplete
// set or this code dropped some, and only the rows distinguish those.
func TestRender_printsTheRowsOfAnUnbalancedVoucher(t *testing.T) {
	ov := overview()
	ov.UnbalancedVouchers = []domain.UnbalancedVoucher{{
		Series: "M", Number: 2, Description: "Lön september", Difference: sek(-110938),
		Rows: []domain.VoucherRow{
			{Account: 7220, Debit: sek(683428), Description: "Lön"},
			{Account: 2710, Credit: sek(185799)},
		},
	}}

	// Accounts and amounts are the diagnostic; the description is redacted by
	// default because production voucher descriptions name counterparties —
	// "Kundbet <customer> (29)" is a real one (#94).
	out := report.Render(ov)
	assert.NotContains(t, out, "Lön september")
	assert.Contains(t, out, "7220")
	assert.Contains(t, out, "683,428.00")
	assert.Contains(t, out, "2710")
	assert.Contains(t, out, "M/2")

	assert.Contains(t, report.RenderWithNames(ov), "Lön september")
}

// A voucher for which no rows came back at all is a different diagnosis from
// one whose rows are merely incomplete, and the report must not render it as
// an empty list that looks like a formatting glitch.
func TestRender_saysWhenNoRowsCameBackAtAll(t *testing.T) {
	ov := overview()
	ov.UnbalancedVouchers = []domain.UnbalancedVoucher{{Series: "A", Number: 144, Difference: sek(-694.30)}}

	assert.Contains(t, report.Render(ov), "no rows returned at all")
}

// #91: "Receivables SEK 0.00" is only meaningful next to the invoice
// population. A reader who sees the zero and stops has been told the company
// is owed nothing, which holds only if unbooked invoices were counted.
func TestRender_zeroReceivablesWithoutAnInvoiceMeasurementSaysUnknown(t *testing.T) {
	ov := overview()
	ov.Receivables = sek(0)
	ov.Payables = sek(0)

	out := report.Render(ov)
	assert.Contains(t, out, "UNKNOWN")
	assert.Contains(t, out, "not measured")
}

func TestRender_unbookedInvoicesAreFlaggedNextToTheZero(t *testing.T) {
	ov := overview()
	ov.Receivables = sek(0)
	ov.Obligations = domain.InvoiceStates{
		Supplier: []domain.InvoiceStateCount{{Filter: "unpaid", Count: 0}, {Filter: "unbooked", Count: 7}},
		Customer: []domain.InvoiceStateCount{{Filter: "unpaid", Count: 0}, {Filter: "unbooked", Count: 0}},
	}

	out := report.Render(ov)
	assert.Contains(t, out, "!! Outstanding:")
	assert.Contains(t, out, "does NOT mean nothing is owed")
	// And the counts themselves, so the claim is checkable.
	assert.Contains(t, out, "unbooked")
}

// A settled company must be able to say so plainly, with its evidence.
func TestRender_settledLedgersStateTheEvidence(t *testing.T) {
	ov := overview()
	ov.Obligations = domain.InvoiceStates{
		Supplier: []domain.InvoiceStateCount{{Filter: "unpaid", Count: 0}, {Filter: "unbooked", Count: 0}, {Filter: "fullypaid", Count: 140}},
		Customer: []domain.InvoiceStateCount{{Filter: "unpaid", Count: 0}, {Filter: "unbooked", Count: 0}, {Filter: "fullypaid", Count: 29}},
	}

	out := report.Render(ov)
	assert.Contains(t, out, "Outstanding: none")
	assert.Contains(t, out, "140")
	assert.NotContains(t, out, "!! Outstanding")
}

// The status table must be one line per filter and stable, since it lands in a
// Job log and gets grepped.
func TestRender_statusTableIsOneLinePerFilter(t *testing.T) {
	ov := overview()
	ov.Obligations = domain.InvoiceStates{
		Supplier: []domain.InvoiceStateCount{{Filter: "unpaid", Count: 0}, {Filter: "unbooked", Count: 2}},
	}

	first := report.Render(ov)
	assert.Equal(t, first, report.Render(ov), "rendering must be deterministic")

	var lines int
	for _, l := range strings.Split(first, "\n") {
		if strings.HasPrefix(l, "  supplier ") {
			lines++
		}
	}
	assert.Equal(t, 2, lines)
}

// A count is a finding; an invoice is actionable. #91 resolved to one unbooked
// supplier and one unbooked customer invoice, and the next question was
// immediately "which, for how much, due when".
func TestRender_namesTheUnbookedInvoices(t *testing.T) {
	ov := overview()
	ov.UnbookedSupplier = []domain.SupplierInvoice{{
		InvoiceNumber: 9001, SupplierName: "Lev AB", SupplierReference: "LEV-77",
		Balance: sek(12500.50), DueDate: "2026-09-30",
	}}
	ov.UnbookedCustomer = []domain.CustomerInvoice{{
		InvoiceNumber: 31, CustomerName: "Kund AB", Balance: sek(18750), DueDate: "2026-10-15",
	}}

	// Default output: everything needed to act, no names (#94).
	out := report.Render(ov)
	assert.Contains(t, out, "UNBOOKED INVOICES")
	assert.Contains(t, out, "9001")
	assert.Contains(t, out, "12,500.50")
	assert.Contains(t, out, "2026-09-30")
	assert.Contains(t, out, "LEV-77", "the supplier's own reference is what they will quote")
	assert.Contains(t, out, "18,750.00")
	assert.NotContains(t, out, "Lev AB")

	named := report.RenderWithNames(ov)
	assert.Contains(t, named, "Lev AB")
	assert.Contains(t, named, "Kund AB")
}

// The section must not appear when there is nothing unbooked — an empty
// heading reads like a failed measurement.
func TestRender_noUnbookedSectionWhenThereAreNone(t *testing.T) {
	assert.NotContains(t, report.Render(overview()), "UNBOOKED INVOICES")
}

// A closed year must say so. Before this, both closed production years
// reported "Result SEK 0.00", which reads as a year that made nothing.
func TestRender_closedYearStatesTheTransfer(t *testing.T) {
	ov := overview()
	ov.Result = sek(665344.17)
	ov.ResultTransferred = sek(665344.17)

	out := report.Render(ov)
	assert.Contains(t, out, "Year CLOSED")
	assert.Contains(t, out, "665,344.17")
	assert.NotContains(t, out, "does not match")
}

// If the transfer and the computed result disagree, the two were derived from
// different things and one is wrong. Saying so beats picking a winner.
func TestRender_transferDisagreeingWithResultIsFlagged(t *testing.T) {
	ov := overview()
	ov.Result = sek(600000)
	ov.ResultTransferred = sek(665344.17)

	assert.Contains(t, report.Render(ov), "does not match")
}

func TestRender_openYearSaysNothingAboutClosing(t *testing.T) {
	assert.NotContains(t, report.Render(overview()), "Year CLOSED")
}
