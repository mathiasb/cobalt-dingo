package domain

// AccountSummary is what a GL account looks like over a period: how many
// journal entries touched it, and when.
//
// Dates are the raw YYYY-MM-DD strings Fortnox returns rather than time.Time.
// Parsing them here would mean deciding what an unparseable date is, and the
// question this answers — "is anything still arriving in this account?" — does
// not need the arithmetic.
type AccountSummary struct {
	Account  int
	Count    int    // vouchers touching the account, not rows
	Earliest string // YYYY-MM-DD, empty when nothing was found
	Latest   string // YYYY-MM-DD, empty when nothing was found
}

// SummariseAccount reports how many vouchers touched an account and over what
// span.
//
// It answers assumption A1 in docs/live-financial-data.md: whether Fortnox
// Bankkoppling is actually feeding account 1930. A populated 1930 means bank
// transactions are already inside Fortnox and can be read through the API we
// have; an empty one means the camt.053 track is required after all. The
// comparison is against what the bank itself shows, which no Fortnox endpoint
// can provide — there is no statement or reconciliation resource in the API.
//
// Counted per voucher, not per row: a single entry with both a debit and a
// credit line on the account is one transaction, and counting rows would
// double it.
func SummariseAccount(vouchers []Voucher, account int) AccountSummary {
	out := AccountSummary{Account: account}
	for _, v := range vouchers {
		if !touchesAccount(v, account) {
			continue
		}
		out.Count++
		// An empty date is missing data, not the beginning of time. Sorting it
		// first would make "earliest" meaningless the moment one voucher lacks
		// a date.
		if v.TransactionDate == "" {
			continue
		}
		if out.Earliest == "" || v.TransactionDate < out.Earliest {
			out.Earliest = v.TransactionDate
		}
		if v.TransactionDate > out.Latest {
			out.Latest = v.TransactionDate
		}
	}
	return out
}

func touchesAccount(v Voucher, account int) bool {
	for _, r := range v.Rows {
		if r.Account == account {
			return true
		}
	}
	return false
}
