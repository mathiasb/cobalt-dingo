package domain

import "testing"

func v(date string, accounts ...int) Voucher {
	rows := make([]VoucherRow, 0, len(accounts))
	for _, a := range accounts {
		rows = append(rows, VoucherRow{Account: a, Debit: MoneyFromFloat(100, "SEK")})
	}
	return Voucher{TransactionDate: date, Rows: rows}
}

// Assumption A1 in docs/live-financial-data.md: is Fortnox Bankkoppling feeding
// account 1930? The answer is a count and a latest date, compared against what
// the bank itself shows.
func TestSummariseAccount_countsVouchersTouchingTheAccount(t *testing.T) {
	got := SummariseAccount([]Voucher{
		v("2026-09-01", 1930, 2440),
		v("2026-09-14", 1930),
		v("2026-08-20", 2440),       // no 1930 row
		v("2026-07-02", 1930, 1930), // two rows, one voucher
	}, 1930)

	if got.Count != 3 {
		t.Errorf("Count = %d, want 3 (a voucher with two rows on the account is still one voucher)", got.Count)
	}
	if got.Latest != "2026-09-14" {
		t.Errorf("Latest = %q, want 2026-09-14", got.Latest)
	}
	if got.Earliest != "2026-07-02" {
		t.Errorf("Earliest = %q, want 2026-07-02", got.Earliest)
	}
}

// An empty account is the answer A1 is actually testing for — it means the bank
// feed is NOT active, which is a finding rather than an error.
func TestSummariseAccount_emptyIsAnAnswerNotAFailure(t *testing.T) {
	got := SummariseAccount(nil, 1930)
	if got.Count != 0 || got.Latest != "" || got.Earliest != "" {
		t.Errorf("want a zero summary, got %+v", got)
	}
}

// Dates are YYYY-MM-DD, so lexicographic comparison is ordering — the same
// assumption the rest of the codebase documents on DueDate.
func TestSummariseAccount_ordersDatesLexicographically(t *testing.T) {
	got := SummariseAccount([]Voucher{
		v("2026-01-09", 1930),
		v("2026-01-10", 1930),
		v("2025-12-31", 1930),
	}, 1930)
	if got.Earliest != "2025-12-31" || got.Latest != "2026-01-10" {
		t.Errorf("got earliest=%q latest=%q", got.Earliest, got.Latest)
	}
}

// A voucher with no date must not become the earliest by sorting before
// everything — an empty string is missing data, not 1 January year zero.
func TestSummariseAccount_ignoresVouchersWithNoDate(t *testing.T) {
	got := SummariseAccount([]Voucher{
		v("", 1930),
		v("2026-05-05", 1930),
	}, 1930)
	if got.Earliest != "2026-05-05" {
		t.Errorf("Earliest = %q, want 2026-05-05 — an undated voucher must not sort first", got.Earliest)
	}
	if got.Count != 2 {
		t.Errorf("Count = %d, want 2 — it still touched the account", got.Count)
	}
}
