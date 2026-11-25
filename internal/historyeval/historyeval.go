// Package historyeval measures how well a customer's own supplier-invoice
// history predicts how the next invoices are booked, and which Fortnox fields
// are actually present in the real data (ADR-0013, spike 1).
//
// It is built to run where the data lives and let only counts out. Report holds
// integers and two dates, no names, numbers, amounts or accounts, and a test
// pins that, so a report cannot carry a customer's ledger lines into a log, a
// terminal or an agent's context.
package historyeval

import (
	"sort"
	"time"

	"github.com/mathiasb/cobalt-dingo/internal/booking"
)

const (
	expenseFirst, expenseLast = 4000, 8999
	topN                      = 3
)

// Row is one accounting row of a supplier invoice. Amounts are minor units.
type Row struct {
	Account       int
	Debit, Credit int64
	CostCenter    bool // the row names a cost centre
	Project       bool // the row names a project
}

// Invoice is one supplier invoice as read from the ERP.
type Invoice struct {
	SupplierNumber int
	SupplierName   string
	Date           string // YYYY-MM-DD
	Currency       string
	Booked         bool
	Cancelled      bool
	VoucherLinked  bool
	Rows           []Row
}

// Supplier carries which identifying fields a supplier record has.
type Supplier struct {
	Number                int
	HasOrganisationNumber bool
	HasVATNumber          bool
	HasBankAccount        bool
	HasIBAN               bool
	PreDefinedAccount     int
}

// Report is everything that leaves the cluster: counts, and two dates.
type Report struct {
	Invoices struct {
		Total                   int
		Booked                  int
		Cancelled               int
		WithRows                int
		WithVoucherLink         int
		WithCostCenterOrProject int
		NonSEK                  int
		BadDate                 int // dates that are not YYYY-MM-DD: counted, never printed, never used
		FirstDate               string
		LastDate                string
	}
	Suppliers struct {
		Total                  int
		WithOrganisationNumber int
		WithVATNumber          int
		WithBankAccount        int
		WithIBAN               int
		WithPredefinedAccount  int
	}
	Eval struct {
		Usable           int // invoices with a derivable expense account, not cancelled
		NoAccount        int // not cancelled, but no expense account could be derived
		CancelledSkipped int
		NoSupplier       int // no supplier number: left out, so they cannot merge into one fictional supplier
		Train            int
	}
	ByNumber   booking.Result // keyed by the ERP's supplier number
	ByName     booking.Result // keyed by supplier name tokens (the booking package's method)
	Predefined struct {
		Covered int // test invoices whose supplier has a predefined account
		Right   int // of those, the predefined account was the booked one
	}
	Diversity struct {
		Suppliers              int // suppliers with at least one usable invoice
		WithMoreThanOneAccount int
	}
}

// Analyse computes the report. testFraction is the share of usable invoices,
// latest by date, held out as the test period; the rest is the training period.
// The split is by time, never random: a random split lets the future teach the
// past and overstates accuracy.
func Analyse(invoices []Invoice, suppliers []Supplier, testFraction float64) Report {
	var r Report
	r.Invoices.Total = len(invoices)
	for _, inv := range invoices {
		if inv.Booked {
			r.Invoices.Booked++
		}
		if inv.Cancelled {
			r.Invoices.Cancelled++
		}
		if len(inv.Rows) > 0 {
			r.Invoices.WithRows++
		}
		if inv.VoucherLinked {
			r.Invoices.WithVoucherLink++
		}
		if hasCostCenterOrProject(inv.Rows) {
			r.Invoices.WithCostCenterOrProject++
		}
		if inv.Currency != "" && inv.Currency != "SEK" {
			r.Invoices.NonSEK++
		}
		if !validDate(inv.Date) {
			r.Invoices.BadDate++
		} else {
			if r.Invoices.FirstDate == "" || inv.Date < r.Invoices.FirstDate {
				r.Invoices.FirstDate = inv.Date
			}
			if inv.Date > r.Invoices.LastDate {
				r.Invoices.LastDate = inv.Date
			}
		}
	}

	predefined := map[int]int{}
	r.Suppliers.Total = len(suppliers)
	for _, s := range suppliers {
		count(&r.Suppliers.WithOrganisationNumber, s.HasOrganisationNumber)
		count(&r.Suppliers.WithVATNumber, s.HasVATNumber)
		count(&r.Suppliers.WithBankAccount, s.HasBankAccount)
		count(&r.Suppliers.WithIBAN, s.HasIBAN)
		if s.PreDefinedAccount != 0 {
			r.Suppliers.WithPredefinedAccount++
			predefined[s.Number] = s.PreDefinedAccount
		}
	}

	type usable struct {
		inv     Invoice
		account int
	}
	var u []usable
	for _, inv := range invoices {
		if inv.Cancelled {
			r.Eval.CancelledSkipped++
			continue
		}
		if !validDate(inv.Date) {
			continue // counted as BadDate above; it cannot be placed in time
		}
		if inv.SupplierNumber == 0 {
			r.Eval.NoSupplier++
			continue
		}
		acct := expenseAccount(inv.Rows)
		if acct == 0 {
			r.Eval.NoAccount++
			continue
		}
		u = append(u, usable{inv, acct})
	}
	r.Eval.Usable = len(u)
	// Date, then supplier, then account: the order must not depend on the order
	// the API happened to return invoices in.
	sort.SliceStable(u, func(i, j int) bool {
		if u[i].inv.Date != u[j].inv.Date {
			return u[i].inv.Date < u[j].inv.Date
		}
		if u[i].inv.SupplierNumber != u[j].inv.SupplierNumber {
			return u[i].inv.SupplierNumber < u[j].inv.SupplierNumber
		}
		return u[i].account < u[j].account
	})

	accountsBySupplier := map[int]map[int]bool{}
	for _, x := range u {
		if accountsBySupplier[x.inv.SupplierNumber] == nil {
			accountsBySupplier[x.inv.SupplierNumber] = map[int]bool{}
		}
		accountsBySupplier[x.inv.SupplierNumber][x.account] = true
	}
	r.Diversity.Suppliers = len(accountsBySupplier)
	for _, accts := range accountsBySupplier {
		if len(accts) > 1 {
			r.Diversity.WithMoreThanOneAccount++
		}
	}

	testN := roundToInt(testFraction * float64(len(u)))
	if testN < 0 {
		testN = 0
	}
	if testN > len(u) {
		testN = len(u)
	}
	train, test := u[:len(u)-testN], u[len(u)-testN:]
	r.Eval.Train = len(train)

	// Keyed by the ERP's supplier number.
	counts := map[int]map[int]int{}
	for _, x := range train {
		if counts[x.inv.SupplierNumber] == nil {
			counts[x.inv.SupplierNumber] = map[int]int{}
		}
		counts[x.inv.SupplierNumber][x.account]++
	}
	// Keyed by name, using the same method the demo uses.
	var examples []booking.Example
	for _, x := range train {
		examples = append(examples, booking.Example{Supplier: x.inv.SupplierName, Account: x.account, Date: x.inv.Date})
	}
	proposer := booking.NewProposer(examples)

	for _, x := range test {
		r.ByNumber.Test++
		ranked := rank(counts[x.inv.SupplierNumber])
		score(&r.ByNumber, ranked, x.account)

		r.ByName.Test++
		var byName []int
		for _, c := range proposer.Propose(x.inv.SupplierName).Candidates {
			byName = append(byName, c.Account)
		}
		score(&r.ByName, byName, x.account)

		if p, ok := predefined[x.inv.SupplierNumber]; ok {
			r.Predefined.Covered++
			if p == x.account {
				r.Predefined.Right++
			}
		}
	}
	return r
}

// score updates res for one test invoice given the proposed accounts, best first.
func score(res *booking.Result, ranked []int, want int) {
	if len(ranked) == 0 {
		res.Unseen++
		return
	}
	for i, a := range ranked {
		if i >= topN {
			break
		}
		if a == want {
			if i == 0 {
				res.Top1++
			}
			res.Top3++
			return
		}
	}
}

// rank orders accounts by how often the supplier was booked to them, ties to
// the lower account number so the result is deterministic.
func rank(counts map[int]int) []int {
	accts := make([]int, 0, len(counts))
	for a := range counts {
		accts = append(accts, a)
	}
	sort.Slice(accts, func(i, j int) bool {
		if counts[accts[i]] != counts[accts[j]] {
			return counts[accts[i]] > counts[accts[j]]
		}
		return accts[i] < accts[j]
	})
	return accts
}

// expenseAccount is the account of the largest debit row in the expense range
// (4000-8999). VAT, bank and supplier-ledger rows are not expense accounts.
func expenseAccount(rows []Row) int {
	best, bestDebit := 0, int64(0)
	for _, row := range rows {
		if row.Account < expenseFirst || row.Account > expenseLast || row.Debit <= 0 {
			continue
		}
		// The largest debit; an equal debit goes to the lower account so the
		// result never depends on row order.
		if row.Debit > bestDebit || (row.Debit == bestDebit && row.Account < best) {
			best, bestDebit = row.Account, row.Debit
		}
	}
	return best
}

func validDate(s string) bool {
	_, err := time.Parse("2006-01-02", s)
	return err == nil
}

func hasCostCenterOrProject(rows []Row) bool {
	for _, row := range rows {
		if row.CostCenter || row.Project {
			return true
		}
	}
	return false
}

func count(n *int, ok bool) {
	if ok {
		*n++
	}
}

func roundToInt(f float64) int {
	if f < 0 {
		return -int(-f + 0.5)
	}
	return int(f + 0.5)
}
