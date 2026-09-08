package statements_test

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mathiasb/cobalt-dingo/internal/statements"
)

func openFixture(t *testing.T) *os.File {
	t.Helper()
	f, err := os.Open("testdata/camt053-sample.xml")
	require.NoError(t, err)
	t.Cleanup(func() { _ = f.Close() })
	return f
}

// TestParseCamt053_BalancesReconcile is the acceptance test named in issue #61:
// the parsed transactions must sum to the statement's closing balance.
//
// It is the only check that catches a parser silently dropping or duplicating
// rows. A parser that returns three of four entries still returns plausible
// transactions; only the balance identity notices.
func TestParseCamt053_BalancesReconcile(t *testing.T) {
	st, err := statements.ParseCamt053(openFixture(t), "camt053-sample.xml")
	require.NoError(t, err)

	require.NoError(t, st.CheckBalance(),
		"opening + net movement must equal closing balance")
}

func TestParseCamt053_StatementMetadata(t *testing.T) {
	st, err := statements.ParseCamt053(openFixture(t), "camt053-sample.xml")
	require.NoError(t, err)

	assert.Equal(t, "SE4550000000058398257466", st.Account)
	assert.Equal(t, "1234567890-2026-08", st.ID)
	assert.Equal(t, "2026-08-01", st.From.Format("2006-01-02"))
	assert.Equal(t, "2026-08-31", st.To.Format("2006-01-02"))
	assert.Equal(t, int64(1250000), st.OpeningBalance.MinorUnits)
	assert.Equal(t, int64(1117550), st.ClosingBalance.MinorUnits)
	assert.Equal(t, "SEK", st.ClosingBalance.Currency)
}

// Pending entries carry a real amount but have not hit the account. Including
// them is the standard way a camt parser breaks the balance identity while
// looking correct.
func TestParseCamt053_ExcludesPendingEntries(t *testing.T) {
	st, err := statements.ParseCamt053(openFixture(t), "camt053-sample.xml")
	require.NoError(t, err)

	require.Len(t, st.Transactions, 3, "the PDNG entry must not be returned as booked")
	for _, tx := range st.Transactions {
		assert.NotEqual(t, "REF-PENDING", tx.BankRef)
	}
}

func TestParseCamt053_TransactionFields(t *testing.T) {
	st, err := statements.ParseCamt053(openFixture(t), "camt053-sample.xml")
	require.NoError(t, err)

	tx := st.Transactions[0]
	assert.Equal(t, statements.Debit, tx.Direction)
	assert.Equal(t, int64(149900), tx.Amount.MinorUnits)
	assert.Equal(t, "SEK", tx.Amount.Currency)
	assert.Equal(t, "2026-08-05", tx.BookingDate.Format("2006-01-02"))
	assert.Equal(t, "REF-0001", tx.BankRef)
	assert.Equal(t, "E2E-HETZNER-08", tx.EndToEndID)
	assert.Equal(t, "Hetzner Online GmbH", tx.Counterparty)
	assert.Equal(t, "Invoice R0012345", tx.Remittance)

	// Provenance: ADR-0002 preservation item 4. A mixed-source period must stay
	// auditable back to the file it came from.
	assert.Equal(t, statements.SourceCamt053, tx.Source)
	assert.Equal(t, "camt053-sample.xml", tx.SourceRef)

	// Counterparty is read from Cdtr for a debit and Dbtr for a credit.
	credit := st.Transactions[2]
	assert.Equal(t, statements.Credit, credit.Direction)
	assert.Equal(t, "Kund AB", credit.Counterparty)
}

// Amounts must be parsed from the decimal string exactly. Going via float64
// makes 325.50 representable only approximately, and a cent lost per row is
// invisible until a balance check fails months later.
func TestParseCamt053_AmountsAreExact(t *testing.T) {
	st, err := statements.ParseCamt053(openFixture(t), "camt053-sample.xml")
	require.NoError(t, err)

	assert.Equal(t, int64(32550), st.Transactions[1].Amount.MinorUnits)
}

// ADR-0002 preservation item 3. Both a manual export and an aggregator will
// re-deliver overlapping ranges; without a stable key, switching sources
// double-counts.
func TestTransaction_IdempotencyKey(t *testing.T) {
	st, err := statements.ParseCamt053(openFixture(t), "camt053-sample.xml")
	require.NoError(t, err)

	seen := map[string]bool{}
	for _, tx := range st.Transactions {
		k := tx.IdempotencyKey()
		require.NotEmpty(t, k)
		require.False(t, seen[k], "idempotency keys must be unique within a statement")
		seen[k] = true
	}

	// Stable across a re-parse of the same file: re-ingesting an overlapping
	// export must not create new rows.
	again, err := statements.ParseCamt053(openFixture(t), "camt053-sample.xml")
	require.NoError(t, err)
	for i, tx := range again.Transactions {
		assert.Equal(t, st.Transactions[i].IdempotencyKey(), tx.IdempotencyKey())
	}

	// The key must not depend on provenance — the same transaction delivered by
	// a different source is still the same transaction. This is what makes
	// switching from manual export to an aggregator safe.
	a := st.Transactions[0]
	b := a
	b.Source = statements.SourceAggregator
	b.SourceRef = "tink-2026-08"
	assert.Equal(t, a.IdempotencyKey(), b.IdempotencyKey())
}

func TestCheckBalance_DetectsADroppedRow(t *testing.T) {
	st, err := statements.ParseCamt053(openFixture(t), "camt053-sample.xml")
	require.NoError(t, err)

	st.Transactions = st.Transactions[:2] // simulate a parser losing the last row
	require.Error(t, st.CheckBalance(),
		"a dropped row must fail the balance identity — this is the point of the check")
}

func TestParseCamt053_RejectsMalformedInput(t *testing.T) {
	_, err := statements.ParseCamt053(strings.NewReader("<Document></Document>"), "empty.xml")
	require.Error(t, err, "a document with no statement is not a valid camt.053")
}
