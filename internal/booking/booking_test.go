package booking_test

import (
	"fmt"
	"testing"

	"github.com/mathiasb/cobalt-dingo/internal/booking"
	"github.com/mathiasb/cobalt-dingo/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func sek(minor int64) domain.Money { return domain.Money{MinorUnits: minor, Currency: "SEK"} }

// supplierVoucher builds a supplier-invoice booking: expense debit, input VAT
// debit, supplier-ledger credit.
func supplierVoucher(series string, n int, date, supplier string, expense int) domain.Voucher {
	return domain.Voucher{
		Series: series, Number: n, TransactionDate: date, Description: supplier,
		Rows: []domain.VoucherRow{
			{Account: expense, Debit: sek(10000)},
			{Account: 2641, Debit: sek(2500)},
			{Account: 2440, Credit: sek(12500)},
		},
	}
}

func TestExamplesFromVouchers_ReadsExpenseAccountOfASupplierInvoiceBooking(t *testing.T) {
	ex := booking.ExamplesFromVouchers([]domain.Voucher{
		supplierVoucher("A", 7, "2026-01-05", "Hetzner Online GmbH", 6540),
	})

	require.Len(t, ex, 1)
	assert.Equal(t, 6540, ex[0].Account)
	assert.Equal(t, 2641, ex[0].VATAccount)
	assert.Equal(t, "A", ex[0].Series)
	assert.Equal(t, 7, ex[0].Number)
	assert.Equal(t, "Hetzner Online GmbH", ex[0].Supplier)
}

func TestExamplesFromVouchers_SkipsVouchersThatAreNotSupplierInvoiceBookings(t *testing.T) {
	bank := domain.Voucher{Series: "B", Number: 1, Description: "Lön", Rows: []domain.VoucherRow{
		{Account: 7210, Debit: sek(500000)},
		{Account: 1930, Credit: sek(500000)},
	}}
	ex := booking.ExamplesFromVouchers([]domain.Voucher{bank})

	assert.Empty(t, ex, "no 2440 credit means it is not a supplier invoice booking")
}

func history() []booking.Example {
	return booking.ExamplesFromVouchers([]domain.Voucher{
		supplierVoucher("A", 1, "2026-01-05", "Hetzner Online GmbH", 6540),
		supplierVoucher("A", 2, "2026-02-05", "Hetzner Online GmbH", 6540),
		supplierVoucher("A", 3, "2026-01-09", "SAS Scandinavian Airlines", 5810),
		supplierVoucher("A", 4, "2026-01-12", "Google Workspace", 5420),
	})
}

func TestPropose_RanksTheHistoricallyBookedAccountFirstForAKnownSupplier(t *testing.T) {
	p := booking.NewProposer(history())

	got := p.Propose("Hetzner Online GmbH")

	require.NotEmpty(t, got.Candidates)
	assert.Equal(t, 6540, got.Candidates[0].Account)
	assert.Greater(t, got.Confidence, 0.5)
}

func TestPropose_MatchesSupplierTextVariants(t *testing.T) {
	p := booking.NewProposer(history())
	tests := []struct{ name, text string }{
		{"lower case", "hetzner online gmbh"},
		{"without the legal suffix", "Hetzner Online"},
		{"with invoice number noise", "Hetzner Online GmbH - Invoice 081000972341"},
		{"with many digit-bearing references", "Hetzner Online GmbH 081000972341 K0499337726 2026-11-20"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := p.Propose(tt.text)

			require.NotEmpty(t, got.Candidates)
			assert.Equal(t, 6540, got.Candidates[0].Account)
		})
	}
}

func TestPropose_ReturnsNoCandidatesAndZeroConfidenceForAnUnseenSupplier(t *testing.T) {
	p := booking.NewProposer(history())

	got := p.Propose("Blomsterhandeln i Lund")

	assert.Empty(t, got.Candidates)
	assert.Zero(t, got.Confidence)
}

func TestPropose_BecauseNamesTheRealPastVouchers(t *testing.T) {
	p := booking.NewProposer(history())

	got := p.Propose("Hetzner Online GmbH")

	var refs []string
	for _, e := range got.Because {
		refs = append(refs, fmt.Sprintf("%s%d", e.Series, e.Number))
	}
	assert.ElementsMatch(t, []string{"A1", "A2"}, refs)
}

func TestLearn_ACorrectionChangesTheNextProposal(t *testing.T) {
	p := booking.NewProposer(history())
	require.Equal(t, 5420, p.Propose("Google Workspace").Candidates[0].Account)

	for i := 0; i < 3; i++ {
		p.Learn(booking.Example{Supplier: "Google Workspace", Account: 6540, VATAccount: 2641})
	}

	assert.Equal(t, 6540, p.Propose("Google Workspace").Candidates[0].Account)
}

func TestEvaluate_SplitsByTimeAndReportsSeenAndUnseenSeparately(t *testing.T) {
	// Train period: Jan. Test period: Feb. The Feb voucher for a supplier
	// first seen in Feb is unseen; the Hetzner one is seen and correct.
	vs := []domain.Voucher{
		supplierVoucher("A", 1, "2026-01-05", "Hetzner Online GmbH", 6540),
		supplierVoucher("A", 2, "2026-01-06", "Hetzner Online GmbH", 6540),
		supplierVoucher("A", 3, "2026-01-07", "SAS Scandinavian Airlines", 5810),
		supplierVoucher("A", 4, "2026-01-08", "Google Workspace", 5420),
		supplierVoucher("A", 5, "2026-02-05", "Hetzner Online GmbH", 6540),
		supplierVoucher("A", 6, "2026-02-06", "Okänd Leverantör AB", 5460),
	}

	res := booking.Evaluate(booking.ExamplesFromVouchers(vs), 2.0/6.0)

	assert.Equal(t, 2, res.Test)
	assert.Equal(t, 1, res.Unseen)
	assert.Equal(t, 1, res.Top1)
	assert.Equal(t, 1, res.Top3)
	assert.InDelta(t, 0.5, res.Top1Rate(), 0.001, "unseen counts as a miss overall")
	assert.InDelta(t, 1.0, res.Top1RateSeen(), 0.001)
}

func TestEvaluate_NeverTrainsOnTheTestPeriod(t *testing.T) {
	// A supplier appearing only in the test period must be unseen. If the
	// split were random or leaked, it would be found in training.
	vs := []domain.Voucher{
		supplierVoucher("A", 1, "2026-01-05", "Hetzner Online GmbH", 6540),
		supplierVoucher("A", 2, "2026-02-05", "Nyfirma Tjänster AB", 6540),
	}

	res := booking.Evaluate(booking.ExamplesFromVouchers(vs), 0.5)

	assert.Equal(t, 1, res.Test)
	assert.Equal(t, 1, res.Unseen)
}

func TestPropose_TreatsAWeakPartialOverlapAsUnseen(t *testing.T) {
	p := booking.NewProposer(history())

	// Shares only "google" with "Google Workspace": overlap 1 of 5 words.
	got := p.Propose("Google Cloud EMEA Services")

	assert.Empty(t, got.Candidates)
}

func TestEvaluate_CountsTop3WhenTheRightAccountIsRankedBelowFirst(t *testing.T) {
	vs := []domain.Voucher{
		supplierVoucher("A", 1, "2026-01-01", "Mixed Supplier", 5410),
		supplierVoucher("A", 2, "2026-01-02", "Mixed Supplier", 5410),
		supplierVoucher("A", 3, "2026-01-03", "Mixed Supplier", 5460),
		supplierVoucher("A", 4, "2026-02-01", "Mixed Supplier", 5460),
	}

	res := booking.Evaluate(booking.ExamplesFromVouchers(vs), 0.25)

	assert.Equal(t, 1, res.Test)
	assert.Equal(t, 0, res.Top1, "5410 outvotes 5460 two to one")
	assert.Equal(t, 1, res.Top3, "but 5460 is still the second candidate")
}
