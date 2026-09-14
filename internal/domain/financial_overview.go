package domain

import (
	"fmt"
	"sort"
	"time"
)

// AccountPosition is one account's movement over a financial year.
//
// Closing is DERIVED here — opening balance plus period movement — and not
// read from Fortnox's BalanceCarriedForward. On an open financial year that
// field is not maintained, and trusting it produced a confident wrong answer
// about account 1930 on 2026-09-14. Deriving it is the reason the voucher
// cache exists: the derivation needs every row, and every row costs a request.
type AccountPosition struct {
	Number      int
	Description string
	Opening     Money
	Debit       Money
	Credit      Money
	Closing     Money
}

// UnbalancedVoucher is a journal entry whose own rows do not sum to zero.
//
// It carries the rows that were actually read. "Voucher A/144 does not
// balance" is not actionable — the next question is always "what did you
// read?", and answering it requires re-fetching the voucher by hand. Fortnox
// will not accept an unbalanced voucher through its own UI, so a difference
// here means either the API returned an incomplete set of rows or this code
// dropped some; both are diagnosed by looking at the rows.
type UnbalancedVoucher struct {
	Series      string
	Number      int
	Description string
	Difference  Money // debit minus credit
	Rows        []VoucherRow
}

// FinancialOverview is the company's position for one financial year.
//
// Freshness, AsOf and Reason are inherited from the VoucherSet it was built
// from and travel with every figure below, because ADR-0006's kill condition
// is a report that presents a cached number without its as-of date.
type FinancialOverview struct {
	Year      int
	Currency  string
	AsOf      time.Time
	Freshness CacheFreshness
	Reason    string

	Positions          []AccountPosition
	UnbalancedVouchers []UnbalancedVoucher
	VoucherCount       int

	// Balance sheet, reported in the sign a reader expects rather than the
	// debit-positive sign the arithmetic uses.
	Assets               Money
	EquityAndLiabilities Money

	// Result. Revenue and Costs are both positive; Result is Revenue − Costs.
	Revenue Money
	Costs   Money
	Result  Money

	// Discrepancy is the sum of every account's closing balance. Double entry
	// makes it zero; anything else means the brought-forward figures or the
	// voucher set are incomplete. Reported rather than absorbed — a balance
	// sheet that always balances because the code made it balance is worthless.
	Discrepancy Money

	Receivables Money
	Payables    Money

	// Obligations is the invoice population by status, which is what makes
	// Receivables and Payables interpretable. Zero receivable can mean
	// "settled" or "every invoice is unbooked", and the two lead to opposite
	// conclusions — see InvoiceStates.Assess (#91).
	//
	// Set separately from BuildFinancialOverview: it is a different
	// measurement, taken from the invoice endpoints rather than derived from
	// the vouchers, so folding it into the ledger arithmetic would imply the
	// two verify each other.
	Obligations InvoiceStates

	// UnbookedSupplier and UnbookedCustomer are the unbooked invoices
	// themselves. A count tells you there is a problem; only the invoices tell
	// you what to do about it (#91).
	UnbookedSupplier []SupplierInvoice
	UnbookedCustomer []CustomerInvoice

	ReceivablesNotYetDue     Money
	ReceivablesOverdue0to30  Money
	ReceivablesOverdue31to90 Money
	ReceivablesOverdue90Plus Money

	PayablesNotYetDue     Money
	PayablesOverdue0to30  Money
	PayablesOverdue31to90 Money
	PayablesOverdue90Plus Money

	receivableInvoices []CustomerInvoice
	payableInvoices    []SupplierInvoice
}

// Balances reports whether the accounting identity holds.
func (ov FinancialOverview) Balances() bool { return ov.Discrepancy.MinorUnits == 0 }

// BuildFinancialOverview computes the year's position from the chart of
// accounts (for opening balances and names) and the year's vouchers.
//
// Pure: no I/O, so every rule below is testable without touching Fortnox.
func BuildFinancialOverview(
	yearID int,
	accounts []Account,
	vouchers VoucherSet,
	receivables []CustomerInvoice,
	payables []SupplierInvoice,
) (FinancialOverview, error) {
	const currency = "SEK"

	type acc struct {
		description   string
		opening       int64
		debit, credit int64
		seen          bool
	}
	byNumber := map[int]*acc{}
	for _, a := range accounts {
		if err := assertCurrency(a.BalanceBF, currency, fmt.Sprintf("account %d opening balance", a.Number)); err != nil {
			return FinancialOverview{}, err
		}
		byNumber[a.Number] = &acc{description: a.Description, opening: a.BalanceBF.MinorUnits, seen: a.BalanceBF.MinorUnits != 0}
	}

	var unbalanced []UnbalancedVoucher
	for _, v := range vouchers.Vouchers {
		var voucherDiff int64
		for _, r := range v.Rows {
			where := fmt.Sprintf("voucher %s/%d account %d", v.Series, v.Number, r.Account)
			if err := assertCurrency(r.Debit, currency, where+" debit"); err != nil {
				return FinancialOverview{}, err
			}
			if err := assertCurrency(r.Credit, currency, where+" credit"); err != nil {
				return FinancialOverview{}, err
			}
			a, ok := byNumber[r.Account]
			if !ok {
				// An account posted to but absent from the chart of accounts.
				// Include it: dropping the row would change the totals while
				// leaving the identity check looking satisfied.
				a = &acc{description: "(not in chart of accounts)"}
				byNumber[r.Account] = a
			}
			a.debit += r.Debit.MinorUnits
			a.credit += r.Credit.MinorUnits
			a.seen = true
			voucherDiff += r.Debit.MinorUnits - r.Credit.MinorUnits
		}
		if voucherDiff != 0 {
			unbalanced = append(unbalanced, UnbalancedVoucher{
				Series:      v.Series,
				Number:      v.Number,
				Description: v.Description,
				Difference:  Money{MinorUnits: voucherDiff, Currency: currency},
				Rows:        v.Rows,
			})
		}
	}

	ov := FinancialOverview{
		Year:               yearID,
		Currency:           currency,
		AsOf:               vouchers.AsOf,
		Freshness:          vouchers.Freshness,
		Reason:             vouchers.Reason,
		UnbalancedVouchers: unbalanced,
		VoucherCount:       len(vouchers.Vouchers),
		receivableInvoices: receivables,
		payableInvoices:    payables,
	}

	var assets, eqLiab, revenue, costs, total int64
	for number, a := range byNumber {
		if !a.seen {
			// No opening balance and no movement. Listing every unused account
			// in the chart would bury the ones that matter.
			continue
		}
		closing := a.opening + a.debit - a.credit
		ov.Positions = append(ov.Positions, AccountPosition{
			Number:      number,
			Description: a.description,
			Opening:     Money{MinorUnits: a.opening, Currency: currency},
			Debit:       Money{MinorUnits: a.debit, Currency: currency},
			Credit:      Money{MinorUnits: a.credit, Currency: currency},
			Closing:     Money{MinorUnits: closing, Currency: currency},
		})
		total += closing

		// BAS account classes. Balance-sheet accounts are 1xxx (assets) and
		// 2xxx (equity and liabilities); 3xxx is revenue; 4xxx–8xxx are costs
		// and financial items.
		switch {
		case number < 2000:
			assets += closing
		case number < 3000:
			eqLiab += closing
		case number < 4000:
			revenue += closing
		default:
			costs += closing
		}
	}

	// Stable order, so two runs of the same report are diffable.
	sort.Slice(ov.Positions, func(i, j int) bool { return ov.Positions[i].Number < ov.Positions[j].Number })

	// Sign flips. Revenue, equity and liabilities are credit-balanced, so
	// their debit-positive totals are negative; reporting them that way is
	// correct and unreadable. Result is Revenue − Costs, which in
	// debit-positive terms is −(revenue + costs).
	ov.Assets = Money{MinorUnits: assets, Currency: currency}
	ov.EquityAndLiabilities = Money{MinorUnits: -eqLiab, Currency: currency}
	ov.Revenue = Money{MinorUnits: -revenue, Currency: currency}
	ov.Costs = Money{MinorUnits: costs, Currency: currency}
	ov.Result = Money{MinorUnits: -revenue - costs, Currency: currency}
	ov.Discrepancy = Money{MinorUnits: total, Currency: currency}

	for _, inv := range receivables {
		if err := assertCurrency(inv.Balance, currency, fmt.Sprintf("customer invoice %d", inv.InvoiceNumber)); err != nil {
			return FinancialOverview{}, err
		}
		ov.Receivables.MinorUnits += inv.Balance.MinorUnits
	}
	ov.Receivables.Currency = currency
	for _, inv := range payables {
		// A cancelled invoice is not an obligation. Fortnox reports the flag
		// and this code used to ignore it, which would have inflated payables
		// by every invoice ever voided.
		if inv.Cancelled {
			continue
		}
		// Balance, not Amount: Balance is what remains owing, so a partly paid
		// invoice would otherwise overstate the obligation by the amount
		// already paid. Fortnox reports both, so using the wrong one is a
		// choice.
		if err := assertCurrency(inv.Balance, currency, fmt.Sprintf("supplier invoice %d", inv.InvoiceNumber)); err != nil {
			return FinancialOverview{}, err
		}
		ov.Payables.MinorUnits += inv.Balance.MinorUnits
	}
	ov.Payables.Currency = currency

	return ov, nil
}

// WithAgeing fills the ageing buckets, relative to today.
//
// Separate from the build because ageing depends on the clock while the ledger
// arithmetic does not — and a report whose numbers change with the time of day
// is harder to reason about than one where only this part does.
func (ov FinancialOverview) WithAgeing(today time.Time) FinancialOverview {
	for _, inv := range ov.receivableInvoices {
		bucket(&ov.ReceivablesNotYetDue, &ov.ReceivablesOverdue0to30,
			&ov.ReceivablesOverdue31to90, &ov.ReceivablesOverdue90Plus,
			inv.DueDate, inv.Balance, today)
	}
	for _, inv := range ov.payableInvoices {
		if inv.Cancelled {
			continue
		}
		bucket(&ov.PayablesNotYetDue, &ov.PayablesOverdue0to30,
			&ov.PayablesOverdue31to90, &ov.PayablesOverdue90Plus,
			inv.DueDate, inv.Balance, today)
	}
	return ov
}

// bucket adds amount to whichever ageing bucket dueDate falls in.
//
// An unparseable due date goes to the MOST overdue bucket. Skipping it would
// drop money out of the total, and treating it as not-yet-due would hide the
// one thing ageing exists to surface.
func bucket(notYetDue, d0to30, d31to90, d90plus *Money, dueDate string, amount Money, today time.Time) {
	*notYetDue = ensureCurrency(*notYetDue, amount.Currency)
	*d0to30 = ensureCurrency(*d0to30, amount.Currency)
	*d31to90 = ensureCurrency(*d31to90, amount.Currency)
	*d90plus = ensureCurrency(*d90plus, amount.Currency)

	due, err := time.Parse("2006-01-02", dueDate)
	if err != nil {
		d90plus.MinorUnits += amount.MinorUnits
		return
	}
	days := int(today.Sub(due).Hours() / 24)
	switch {
	case days <= 0:
		notYetDue.MinorUnits += amount.MinorUnits
	case days <= 30:
		d0to30.MinorUnits += amount.MinorUnits
	case days <= 90:
		d31to90.MinorUnits += amount.MinorUnits
	default:
		d90plus.MinorUnits += amount.MinorUnits
	}
}

func ensureCurrency(m Money, currency string) Money {
	if m.Currency == "" {
		m.Currency = currency
	}
	return m
}

// assertCurrency refuses a figure that is not in the expected currency.
//
// Summing minor units across currencies produces a number that looks like
// money and means nothing. Fortnox keeps the general ledger in company
// currency, so a foreign-currency amount here means an assumption broke — and
// the report must stop rather than present the skew.
func assertCurrency(m Money, expected, where string) error {
	if m.MinorUnits == 0 && m.Currency == "" {
		return nil
	}
	if m.Currency != expected {
		return fmt.Errorf("%s is in %s, expected %s — refusing to sum mixed currencies", where, m.Currency, expected)
	}
	return nil
}
