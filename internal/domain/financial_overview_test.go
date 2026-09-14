package domain_test

import (
	"testing"
	"time"

	"github.com/mathiasb/cobalt-dingo/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func sek(amount float64) domain.Money { return domain.MoneyFromFloat(amount, "SEK") }

func row(account int, debit, credit float64) domain.VoucherRow {
	return domain.VoucherRow{Account: account, Debit: sek(debit), Credit: sek(credit)}
}

// A balanced minimal year: opening cash, one sale, one cost.
func balancedYear() ([]domain.Account, domain.VoucherSet) {
	accounts := []domain.Account{
		{Number: 1930, Description: "Företagskonto", BalanceBF: sek(435527.94)},
		{Number: 2081, Description: "Aktiekapital", BalanceBF: sek(-25000)},
		{Number: 2098, Description: "Vinst föregående år", BalanceBF: sek(-410527.94)},
		{Number: 3011, Description: "Konsultarvoden"},
		{Number: 6570, Description: "Bankkostnader"},
	}
	set := domain.VoucherSet{
		Freshness: domain.CacheFresh,
		AsOf:      time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC),
		Vouchers: []domain.Voucher{
			{Series: "A", Number: 1, TransactionDate: "2026-03-01", Rows: []domain.VoucherRow{
				row(1930, 100000, 0), row(3011, 0, 100000),
			}},
			{Series: "A", Number: 2, TransactionDate: "2026-03-02", Rows: []domain.VoucherRow{
				row(6570, 75, 0), row(1930, 0, 75),
			}},
		},
	}
	return accounts, set
}

// The core of the whole report: a closing balance this code DERIVED, rather
// than Fortnox's BalanceCarriedForward — which is not set on an open year and
// produced a confident wrong answer about account 1930 on 2026-09-14.
func TestFinancialOverview_closingIsOpeningPlusMovement(t *testing.T) {
	accounts, set := balancedYear()

	ov, err := domain.BuildFinancialOverview(2, accounts, set, nil, nil)
	require.NoError(t, err)

	cash := positionFor(t, ov, 1930)
	assert.Equal(t, sek(435527.94), cash.Opening)
	assert.Equal(t, sek(100000), cash.Debit)
	assert.Equal(t, sek(75), cash.Credit)
	assert.Equal(t, sek(535452.94), cash.Closing, "435,527.94 + 100,000 − 75")
}

// Double-entry means every account balance in the year must sum to zero. If it
// does not, something is missing — and the honest output is the discrepancy,
// not a tidy balance sheet that silently absorbs it.
func TestFinancialOverview_accountingIdentityHoldsOnABalancedYear(t *testing.T) {
	accounts, set := balancedYear()

	ov, err := domain.BuildFinancialOverview(2, accounts, set, nil, nil)
	require.NoError(t, err)

	assert.Zero(t, ov.Discrepancy.MinorUnits)
	assert.True(t, ov.Balances())
}

func TestFinancialOverview_discrepancyIsReportedNotAbsorbed(t *testing.T) {
	accounts, set := balancedYear()
	// An opening balance that does not balance — the shape of a year whose
	// brought-forward figures are incomplete.
	accounts[1].BalanceBF = sek(-20000)

	ov, err := domain.BuildFinancialOverview(2, accounts, set, nil, nil)
	require.NoError(t, err)

	assert.Equal(t, sek(5000), ov.Discrepancy)
	assert.False(t, ov.Balances())
}

// A voucher whose own rows do not balance is a data defect worth naming. It
// cannot be posted through Fortnox's UI, but it can arrive through the API,
// and an overview that quietly sums it is how it stays invisible.
func TestFinancialOverview_unbalancedVoucherIsNamed(t *testing.T) {
	accounts, set := balancedYear()
	set.Vouchers[0].Rows[1] = row(3011, 0, 90000)

	ov, err := domain.BuildFinancialOverview(2, accounts, set, nil, nil)
	require.NoError(t, err)

	require.Len(t, ov.UnbalancedVouchers, 1)
	assert.Equal(t, "A", ov.UnbalancedVouchers[0].Series)
	assert.Equal(t, 1, ov.UnbalancedVouchers[0].Number)
	assert.Equal(t, sek(10000), ov.UnbalancedVouchers[0].Difference)
}

// Swedish sign convention: revenue is credit-balanced, so a debit-positive
// closing balance on a 3xxx account is negative. Reporting revenue as a
// negative number is technically right and useless, so the report flips it —
// and the test pins the direction, because getting it backwards produces a
// plausible loss out of a profitable year.
func TestFinancialOverview_revenueAndResultUseCreditPositiveSigns(t *testing.T) {
	accounts, set := balancedYear()

	ov, err := domain.BuildFinancialOverview(2, accounts, set, nil, nil)
	require.NoError(t, err)

	assert.Equal(t, sek(100000), ov.Revenue)
	assert.Equal(t, sek(75), ov.Costs)
	assert.Equal(t, sek(99925), ov.Result)
	assert.Equal(t, sek(535452.94), ov.Assets)
	// Equity and liabilities reported credit-positive: 25,000 + 410,527.94.
	assert.Equal(t, sek(435527.94), ov.EquityAndLiabilities)
}

// ADR-0006's kill condition is a figure presented without its as-of date. The
// overview therefore inherits both from the voucher set rather than deciding
// for itself.
func TestFinancialOverview_inheritsFreshnessAndAsOf(t *testing.T) {
	accounts, set := balancedYear()
	set.Freshness = domain.CacheUnverified
	set.Reason = "Fortnox unreachable"

	ov, err := domain.BuildFinancialOverview(2, accounts, set, nil, nil)
	require.NoError(t, err)

	assert.Equal(t, domain.CacheUnverified, ov.Freshness)
	assert.Equal(t, "Fortnox unreachable", ov.Reason)
	assert.Equal(t, set.AsOf, ov.AsOf)
}

// Adding EUR to SEK by summing minor units yields a number with no meaning.
// Fortnox posts the general ledger in company currency, so a foreign-currency
// row here means an assumption broke — which must stop the report, not skew it.
func TestFinancialOverview_refusesToSumMixedCurrencies(t *testing.T) {
	accounts, set := balancedYear()
	set.Vouchers[0].Rows[0].Debit = domain.MoneyFromFloat(100000, "EUR")

	_, err := domain.BuildFinancialOverview(2, accounts, set, nil, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "EUR")
}

// Receivables and payables come from the live invoice ledgers, not the cache,
// so they carry their own as-of moment implicitly. Ageing buckets are what
// make them actionable.
func TestFinancialOverview_agesReceivablesAndPayables(t *testing.T) {
	accounts, set := balancedYear()
	today := time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC)

	ar := []domain.CustomerInvoice{
		{InvoiceNumber: 1, CustomerName: "Kund A", Balance: sek(12500), DueDate: "2026-09-30"},
		{InvoiceNumber: 2, CustomerName: "Kund B", Balance: sek(7000), DueDate: "2026-09-01"},
		{InvoiceNumber: 3, CustomerName: "Kund C", Balance: sek(3000), DueDate: "2026-06-01"},
	}
	ap := []domain.SupplierInvoice{
		{InvoiceNumber: 10, SupplierName: "Lev A", Amount: sek(4000), DueDate: "2026-09-20"},
	}

	ov, err := domain.BuildFinancialOverview(2, accounts, set, ar, ap)
	require.NoError(t, err)
	ov = ov.WithAgeing(today)

	assert.Equal(t, sek(22500), ov.Receivables)
	assert.Equal(t, sek(4000), ov.Payables)
	assert.Equal(t, sek(12500), ov.ReceivablesNotYetDue)
	assert.Equal(t, sek(7000), ov.ReceivablesOverdue0to30)
	assert.Equal(t, sek(3000), ov.ReceivablesOverdue90Plus)
}

func positionFor(t *testing.T, ov domain.FinancialOverview, account int) domain.AccountPosition {
	t.Helper()
	for _, p := range ov.Positions {
		if p.Number == account {
			return p
		}
	}
	t.Fatalf("no position for account %d", account)
	return domain.AccountPosition{}
}

// A row posted to an account missing from the chart of accounts must still
// count. Dropping it would change every total while leaving the accounting
// identity looking satisfied — the report would balance BECAUSE it discarded
// the evidence.
func TestFinancialOverview_accountAbsentFromTheChartStillCounts(t *testing.T) {
	accounts, set := balancedYear()
	set.Vouchers = append(set.Vouchers, domain.Voucher{
		Series: "A", Number: 3, TransactionDate: "2026-04-01",
		Rows: []domain.VoucherRow{row(1930, 0, 500), row(6999, 500, 0)},
	})

	ov, err := domain.BuildFinancialOverview(2, accounts, set, nil, nil)
	require.NoError(t, err)

	orphan := positionFor(t, ov, 6999)
	assert.Equal(t, sek(500), orphan.Closing)
	assert.Contains(t, orphan.Description, "not in chart")
	assert.Zero(t, ov.Discrepancy.MinorUnits, "the identity must still hold with the orphan counted")
	assert.Equal(t, sek(575), ov.Costs)
}

// An unparseable due date goes to the most-overdue bucket. Skipping it drops
// money out of the total; calling it not-yet-due hides exactly what ageing is
// for.
func TestFinancialOverview_unparseableDueDateAgesToTheWorstBucket(t *testing.T) {
	accounts, set := balancedYear()
	ar := []domain.CustomerInvoice{{InvoiceNumber: 9, Balance: sek(1000), DueDate: ""}}

	ov, err := domain.BuildFinancialOverview(2, accounts, set, ar, nil)
	require.NoError(t, err)
	ov = ov.WithAgeing(time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC))

	assert.Equal(t, sek(1000), ov.ReceivablesOverdue90Plus)
	assert.Zero(t, ov.ReceivablesNotYetDue.MinorUnits)
	assert.Equal(t, ov.Receivables.MinorUnits,
		ov.ReceivablesNotYetDue.MinorUnits+ov.ReceivablesOverdue0to30.MinorUnits+
			ov.ReceivablesOverdue31to90.MinorUnits+ov.ReceivablesOverdue90Plus.MinorUnits,
		"buckets must account for every öre of the total")
}
