package process_test

import (
	"context"
	"errors"
	"testing"

	"github.com/mathiasb/cobalt-dingo/internal/booking"
	"github.com/mathiasb/cobalt-dingo/internal/domain"
	"github.com/mathiasb/cobalt-dingo/internal/extract"
	"github.com/mathiasb/cobalt-dingo/internal/process"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeExtractor struct {
	inv extract.Invoice
	err error
}

func (f fakeExtractor) Extract(context.Context, []byte) (extract.Invoice, error) { return f.inv, f.err }

func proposer() *booking.Proposer {
	sek := func(m int64) domain.Money { return domain.Money{MinorUnits: m, Currency: "SEK"} }
	return booking.NewProposer(booking.ExamplesFromVouchers([]domain.Voucher{{
		Series: "A", Number: 1, TransactionDate: "2026-01-05", Description: "Hetzner Online GmbH",
		Rows: []domain.VoucherRow{
			{Account: 6540, Debit: sek(10000)},
			{Account: 2641, Debit: sek(2500)},
			{Account: 2440, Credit: sek(12500)},
		},
	}}))
}

func reconciled() extract.Invoice {
	return extract.Invoice{
		Supplier: "Hetzner Online GmbH", InvoiceNumber: "1", InvoiceDate: "2026-11-02", Currency: "SEK",
		NetTotal: 10000, NetKnown: true, VATAmount: 2500, Total: 12500,
	}
}

func TestRun_ProposesFromTheReadSupplierAndIsTrustedWhenTheInvoiceReconciles(t *testing.T) {
	res, err := process.Run(context.Background(), []byte("img"), fakeExtractor{inv: reconciled()}, proposer())

	require.NoError(t, err)
	require.NotEmpty(t, res.Proposal.Candidates)
	assert.Equal(t, 6540, res.Proposal.Candidates[0].Account)
	assert.False(t, res.DoNotTrust)
	assert.Empty(t, res.Warnings)
}

func TestRun_MarksTheProposalDoNotTrustWhenTheInvoiceDoesNotReconcile(t *testing.T) {
	inv := reconciled()
	inv.Total = 1250 // a dropped digit

	res, err := process.Run(context.Background(), []byte("img"), fakeExtractor{inv: inv}, proposer())

	require.NoError(t, err)
	assert.True(t, res.DoNotTrust)
	require.NotEmpty(t, res.Warnings)
	assert.Contains(t, res.Warnings[0], "does not equal")
	assert.NotEmpty(t, res.Proposal.Candidates, "the proposal is still shown so a person can see what was read, but it is marked")
}

func TestRun_AnAmbiguousDateIsAWarningButNotDoNotTrust(t *testing.T) {
	inv := reconciled()
	inv.InvoiceDateAmbiguous = true

	res, err := process.Run(context.Background(), []byte("img"), fakeExtractor{inv: inv}, proposer())

	require.NoError(t, err)
	assert.False(t, res.DoNotTrust)
	require.Len(t, res.Warnings, 1)
	assert.Contains(t, res.Warnings[0], "invoice date")
}

func TestRun_ReturnsTheExtractionErrorWithoutAProposal(t *testing.T) {
	_, err := process.Run(context.Background(), []byte("img"), fakeExtractor{err: errors.New("model down")}, proposer())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "model down")
}

func TestRun_AnUnseenSupplierGetsNoCandidatesRatherThanAGuess(t *testing.T) {
	inv := reconciled()
	inv.Supplier = "Blomsterhandeln i Lund"

	res, err := process.Run(context.Background(), []byte("img"), fakeExtractor{inv: inv}, proposer())

	require.NoError(t, err)
	assert.Empty(t, res.Proposal.Candidates)
}

func TestAssess_MarksAnUnreconciledInvoiceDoNotTrustAndStillProposes(t *testing.T) {
	inv := reconciled()
	inv.Total = 1250

	got := process.Assess(inv, proposer())

	assert.True(t, got.DoNotTrust)
	require.NotEmpty(t, got.Proposal.Candidates)
	assert.Equal(t, 6540, got.Proposal.Candidates[0].Account)
	require.NotEmpty(t, got.Warnings)
}
