package historyeval_test

import (
	"reflect"
	"testing"

	"github.com/mathiasb/cobalt-dingo/internal/historyeval"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// inv builds a booked supplier invoice whose largest expense debit is `account`.
func inv(supplier int, name, date string, account int) historyeval.Invoice {
	return historyeval.Invoice{
		SupplierNumber: supplier, SupplierName: name, Date: date, Currency: "SEK", Booked: true,
		VoucherLinked: true,
		Rows: []historyeval.Row{
			{Account: account, Debit: 1000},
			{Account: 2641, Debit: 250},
			{Account: 2440, Credit: 1250},
		},
	}
}

func TestAnalyse_CountsThePopulationAndHowMuchOfItIsUsable(t *testing.T) {
	cancelled := inv(1, "Alfa AB", "2026-01-03", 6540)
	cancelled.Cancelled = true
	unbooked := inv(2, "Beta Ltd", "2026-03-09", 5460)
	unbooked.Booked, unbooked.VoucherLinked = false, false
	noRows := inv(3, "Gamma", "2026-02-01", 0)
	noRows.Rows = nil
	withProject := inv(4, "Delta", "2026-02-20", 5810)
	withProject.Rows[0].Project = true
	eur := inv(5, "Eps", "2026-02-25", 5420)
	eur.Currency = "EUR"

	r := historyeval.Analyse([]historyeval.Invoice{cancelled, unbooked, noRows, withProject, eur}, nil, 0.2)

	assert.Equal(t, 5, r.Invoices.Total)
	assert.Equal(t, 1, r.Invoices.Cancelled)
	assert.Equal(t, 4, r.Invoices.Booked, "cancelled counts as booked here: the flag is what Fortnox returned")
	assert.Equal(t, 4, r.Invoices.WithRows)
	assert.Equal(t, 4, r.Invoices.WithVoucherLink)
	assert.Equal(t, 1, r.Invoices.WithCostCenterOrProject)
	assert.Equal(t, 1, r.Invoices.NonSEK)
	assert.Equal(t, "2026-01-03", r.Invoices.FirstDate)
	assert.Equal(t, "2026-03-09", r.Invoices.LastDate)
}

func TestAnalyse_CountsWhichIdentifiersTheSuppliersCarry(t *testing.T) {
	sup := []historyeval.Supplier{
		{Number: 1, HasOrganisationNumber: true, HasVATNumber: true, HasBankAccount: true, HasIBAN: true, PreDefinedAccount: 6540},
		{Number: 2, HasOrganisationNumber: true},
		{Number: 3},
	}

	r := historyeval.Analyse(nil, sup, 0.2)

	assert.Equal(t, 3, r.Suppliers.Total)
	assert.Equal(t, 2, r.Suppliers.WithOrganisationNumber)
	assert.Equal(t, 1, r.Suppliers.WithVATNumber)
	assert.Equal(t, 1, r.Suppliers.WithBankAccount)
	assert.Equal(t, 1, r.Suppliers.WithIBAN)
	assert.Equal(t, 1, r.Suppliers.WithPredefinedAccount)
}

func TestAnalyse_TheExpenseAccountIsTheLargestDebitInTheExpenseRangeAndNotVATOrTheSupplierLedger(t *testing.T) {
	i := historyeval.Invoice{
		SupplierNumber: 1, SupplierName: "Alfa", Date: "2026-01-01", Currency: "SEK", Booked: true,
		Rows: []historyeval.Row{
			{Account: 2641, Debit: 9999},  // VAT: larger, but not an expense account
			{Account: 5460, Debit: 400},   // expense
			{Account: 6540, Debit: 600},   // larger expense: this one
			{Account: 1930, Debit: 99999}, // bank: not an expense account
			{Account: 2440, Credit: 11000},
		},
	}
	later := inv(1, "Alfa", "2026-02-01", 6540)

	r := historyeval.Analyse([]historyeval.Invoice{i, later}, nil, 0.5)

	assert.Equal(t, 1, r.ByNumber.Test)
	assert.Equal(t, 1, r.ByNumber.Top1, "the earlier invoice was learned as 6540")
}

func TestAnalyse_InvoicesWithNoDerivableAccountAreCountedAndLeftOutOfTheEvaluation(t *testing.T) {
	noRows := inv(1, "Alfa", "2026-01-01", 0)
	noRows.Rows = nil
	cancelled := inv(2, "Beta", "2026-01-02", 5460)
	cancelled.Cancelled = true
	ok := inv(3, "Gamma", "2026-01-03", 6540)

	r := historyeval.Analyse([]historyeval.Invoice{noRows, cancelled, ok}, nil, 0.5)

	assert.Equal(t, 1, r.Eval.NoAccount, "no rows")
	assert.Equal(t, 1, r.Eval.CancelledSkipped)
	assert.Equal(t, 1, r.Eval.Usable)
}

func TestAnalyse_SplitsByTimeAndScoresSeenAndUnseenSuppliersByNumber(t *testing.T) {
	var all []historyeval.Invoice
	// supplier 1 always 6540, supplier 2 mostly 5460; the latest invoices are the test period.
	all = append(all, inv(1, "Alfa AB", "2026-01-05", 6540), inv(1, "Alfa AB", "2026-01-06", 6540),
		inv(2, "Beta Ltd", "2026-01-07", 5460), inv(2, "Beta Ltd", "2026-01-08", 5460),
		inv(1, "Alfa AB", "2026-02-05", 6540),  // test: right
		inv(2, "Beta Ltd", "2026-02-06", 5420), // test: seen supplier, a different account: wrong
		inv(9, "Nyfirma", "2026-02-07", 6540))  // test: unseen supplier

	r := historyeval.Analyse(all, nil, 3.0/7.0)

	assert.Equal(t, 4, r.Eval.Train)
	assert.Equal(t, 3, r.ByNumber.Test)
	assert.Equal(t, 1, r.ByNumber.Unseen)
	assert.Equal(t, 1, r.ByNumber.Top1)
	assert.Equal(t, 1, r.ByNumber.Top3, "the wrong one is not in the supplier's top three either")
}

func TestAnalyse_NeverTrainsOnTheTestPeriod(t *testing.T) {
	all := []historyeval.Invoice{
		inv(1, "Alfa", "2026-01-01", 6540),
		inv(2, "Nyfirma", "2026-02-01", 5460), // first seen in the test period: must be unseen
	}

	r := historyeval.Analyse(all, nil, 0.5)

	assert.Equal(t, 1, r.ByNumber.Test)
	assert.Equal(t, 1, r.ByNumber.Unseen)
}

func TestAnalyse_KeyingBySupplierNumberSurvivesARenameThatBreaksKeyingByName(t *testing.T) {
	all := []historyeval.Invoice{
		inv(1, "Alfa AB", "2026-01-01", 6540),
		inv(1, "Alfa AB", "2026-01-02", 6540),
		inv(1, "Zeta Industrier Holding", "2026-02-01", 6540), // same supplier, renamed
	}

	r := historyeval.Analyse(all, nil, 1.0/3.0)

	assert.Equal(t, 1, r.ByNumber.Top1, "the number still identifies the supplier")
	assert.Equal(t, 1, r.ByName.Unseen, "the new name matches nothing the proposer learned")
}

func TestAnalyse_TheERPsPredefinedAccountIsScoredAsABaselineOnlyWhereItExists(t *testing.T) {
	all := []historyeval.Invoice{
		inv(1, "Alfa", "2026-01-01", 6540),
		inv(1, "Alfa", "2026-02-01", 6540), // test: predefined 6540 is right
		inv(2, "Beta", "2026-01-02", 5460),
		inv(2, "Beta", "2026-02-02", 5810), // test: predefined 5460 is wrong
		inv(3, "Gamma", "2026-01-03", 5420),
		inv(3, "Gamma", "2026-02-03", 5420), // test: no predefined: not scored
	}
	sup := []historyeval.Supplier{{Number: 1, PreDefinedAccount: 6540}, {Number: 2, PreDefinedAccount: 5460}, {Number: 3}}

	r := historyeval.Analyse(all, sup, 0.5)

	assert.Equal(t, 2, r.Predefined.Covered)
	assert.Equal(t, 1, r.Predefined.Right)
}

func TestAnalyse_CountsSuppliersBookedToMoreThanOneAccountAsAMeasureOfHowHardItIs(t *testing.T) {
	all := []historyeval.Invoice{
		inv(1, "Alfa", "2026-01-01", 6540), inv(1, "Alfa", "2026-01-02", 6540),
		inv(2, "Beta", "2026-01-03", 5460), inv(2, "Beta", "2026-01-04", 5810),
		inv(3, "Gamma", "2026-01-05", 5420),
	}

	r := historyeval.Analyse(all, nil, 0.2)

	assert.Equal(t, 3, r.Diversity.Suppliers)
	assert.Equal(t, 1, r.Diversity.WithMoreThanOneAccount)
}

func TestAnalyse_OnNoInputItReturnsAZeroReportWithoutPanicking(t *testing.T) {
	r := historyeval.Analyse(nil, nil, 0.2)

	assert.Equal(t, historyeval.Report{}, r)
}

// The report is what leaves the cluster. Its type can hold counts and two
// dates (the first and last invoice date) and nothing else, so no supplier
// name, number, amount or account can be carried out through it.
func TestReport_HoldsOnlyCountsAndTwoDatesAndNoSupplierDataOfAnyKind(t *testing.T) {
	var walk func(t reflect.Type, path string)
	walk = func(rt reflect.Type, path string) {
		k := rt.Kind()
		if k == reflect.Struct {
			for i := 0; i < rt.NumField(); i++ {
				walk(rt.Field(i).Type, path+"."+rt.Field(i).Name)
			}
			return
		}
		if k == reflect.Int {
			return
		}
		if k == reflect.String {
			assert.Contains(t, []string{".Invoices.FirstDate", ".Invoices.LastDate"}, path, "only the two dates may be strings")
			return
		}
		require.Failf(t, "report field is not a count", "%s has kind %s", path, k)
	}

	walk(reflect.TypeOf(historyeval.Report{}), "")
}

func TestAnalyse_AnAccountRankedSecondCountsForTop3AndNotForTop1(t *testing.T) {
	all := []historyeval.Invoice{
		inv(1, "Alfa", "2026-01-01", 5460), inv(1, "Alfa", "2026-01-02", 5460), inv(1, "Alfa", "2026-01-03", 5810),
		inv(1, "Alfa", "2026-02-01", 5810), // test: the supplier's second most common account
	}

	r := historyeval.Analyse(all, nil, 0.25)

	assert.Equal(t, 1, r.ByNumber.Test)
	assert.Equal(t, 0, r.ByNumber.Top1)
	assert.Equal(t, 1, r.ByNumber.Top3)
}

func TestAnalyse_AnAccountOutsideTheTopThreeCountsForNothing(t *testing.T) {
	all := []historyeval.Invoice{
		inv(1, "Alfa", "2026-01-01", 5460), inv(1, "Alfa", "2026-01-02", 5460), inv(1, "Alfa", "2026-01-03", 5460),
		inv(1, "Alfa", "2026-01-04", 5810), inv(1, "Alfa", "2026-01-05", 5810),
		inv(1, "Alfa", "2026-01-06", 6540), inv(1, "Alfa", "2026-01-07", 6540),
		inv(1, "Alfa", "2026-01-08", 7010), // the fourth account, seen once
		inv(1, "Alfa", "2026-02-01", 7010), // test
	}

	r := historyeval.Analyse(all, nil, 1.0/9.0)

	assert.Equal(t, 0, r.ByNumber.Top3)
	assert.Equal(t, 0, r.ByNumber.Unseen, "the supplier is known, the account just ranks fourth")
}

func TestAnalyse_WhenTwoAccountsAreEquallyCommonTheLowerNumberIsRankedFirst(t *testing.T) {
	train := []historyeval.Invoice{inv(1, "Alfa", "2026-01-01", 5810), inv(1, "Alfa", "2026-01-02", 5460)}
	low := append(append([]historyeval.Invoice{}, train...), inv(1, "Alfa", "2026-02-01", 5460))
	high := append(append([]historyeval.Invoice{}, train...), inv(1, "Alfa", "2026-02-01", 5810))

	rLow := historyeval.Analyse(low, nil, 1.0/3.0)
	rHigh := historyeval.Analyse(high, nil, 1.0/3.0)

	assert.Equal(t, 1, rLow.ByNumber.Top1, "the tie goes to the lower account, so a result never depends on map order")
	assert.Equal(t, 0, rHigh.ByNumber.Top1)
	assert.Equal(t, 1, rHigh.ByNumber.Top3)
}

func TestAnalyse_TheNameKeyedEvaluationCountsTheSameTestInvoicesAsTheNumberKeyedOne(t *testing.T) {
	all := []historyeval.Invoice{
		inv(1, "Alfa AB", "2026-01-05", 6540), inv(1, "Alfa AB", "2026-01-06", 6540),
		inv(1, "Alfa AB", "2026-02-05", 6540), inv(2, "Beta Ltd", "2026-02-06", 5460),
	}

	r := historyeval.Analyse(all, nil, 0.5)

	assert.Equal(t, 2, r.ByNumber.Test)
	assert.Equal(t, 2, r.ByName.Test)
	assert.Equal(t, 1, r.ByName.Top1)
	assert.Equal(t, 1, r.ByName.Unseen)
}

func TestRender_PrintsLabelledCountsAndRatesAndNAWhereThereIsNothingToDivide(t *testing.T) {
	var r historyeval.Report
	r.Invoices.Total = 156
	r.Invoices.WithRows = 150
	r.Invoices.WithVoucherLink = 140
	r.Invoices.FirstDate, r.Invoices.LastDate = "2025-01-03", "2026-11-20"
	r.Suppliers.Total = 40
	r.Suppliers.WithOrganisationNumber = 30
	r.ByNumber.Test, r.ByNumber.Top1, r.ByNumber.Top3, r.ByNumber.Unseen = 30, 24, 28, 3
	// ByName, Predefined and Diversity left at zero on purpose.

	out := historyeval.Render(r)

	assert.Contains(t, out, "invoices: 156")
	assert.Contains(t, out, "with rows: 96.2% (150/156)")
	assert.Contains(t, out, "with voucher link: 89.7% (140/156)")
	assert.Contains(t, out, "2025-01-03 to 2026-11-20")
	assert.Contains(t, out, "organisation number: 75.0% (30/40)")
	assert.Contains(t, out, "top-1 80.0% (24/30)")
	assert.Contains(t, out, "top-3 93.3% (28/30)")
	assert.Contains(t, out, "unseen suppliers 3")
	assert.Contains(t, out, "n/a", "a zero denominator is n/a, not 0% and not NaN")
	assert.NotContains(t, out, "NaN")
}

func TestRender_SaysPlainlyWhatTheNumbersAreAndAreNot(t *testing.T) {
	out := historyeval.Render(historyeval.Report{})

	assert.Contains(t, out, "aggregates only")
	assert.Contains(t, out, "held-out")
	assert.Contains(t, out, "not a guarantee")
}
