package payment_test

import (
	"testing"
	"time"

	"github.com/mathiasb/cobalt-dingo/internal/domain"
	"github.com/mathiasb/cobalt-dingo/internal/payment"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func debtor() payment.Debtor {
	return payment.Debtor{
		Name: "Definitely Mabe AB",
		IBAN: "SE3550000000054910000003",
		BIC:  "ESSESESS",
	}
}

func enriched(amount domain.Money) domain.EnrichedInvoice {
	return domain.EnrichedInvoice{
		SupplierInvoice: domain.SupplierInvoice{
			InvoiceNumber: 1042,
			SupplierName:  "Acme GmbH",
			Amount:        amount,
			Balance:       amount,
			DueDate:       "2026-10-01",
		},
		IBAN: "DE89370400440532013000",
		BIC:  "COBADEFFXXX",
	}
}

// This was not hypothetical. Until 2026-09-14 every supplier invoice read from
// Fortnox had Amount SEK 0.00, because the code read TotalInvoiceCurrency — a
// field Fortnox sends from no endpoint. A payment file generated in that state
// would have instructed a bank to pay zero against real invoices, and the
// amount is the one field nobody double-checks when the supplier and IBAN look
// right.
//
// The decode is fixed. This refuses the state regardless, because the next
// cause of a zero amount will be a different one.
func TestGeneratePAIN001_refusesAZeroAmount(t *testing.T) {
	_, err := payment.GeneratePAIN001(
		[]domain.EnrichedInvoice{enriched(domain.MoneyFromFloat(0, "EUR"))},
		debtor(), "MSG-1", time.Now())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "1042")
	assert.Contains(t, err.Error(), "non-positive")
}

// A negative instructed amount is not a refund in pain.001 — it is a malformed
// instruction.
func TestGeneratePAIN001_refusesANegativeAmount(t *testing.T) {
	_, err := payment.GeneratePAIN001(
		[]domain.EnrichedInvoice{enriched(domain.MoneyFromFloat(-100, "EUR"))},
		debtor(), "MSG-1", time.Now())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "1042")
}

// One bad invoice must fail the whole file rather than being dropped. A batch
// silently missing a payment is worse than one that refuses to build: the
// supplier does not get paid and nothing says so.
func TestGeneratePAIN001_oneZeroAmountFailsTheWholeBatch(t *testing.T) {
	good := enriched(domain.MoneyFromFloat(2450, "EUR"))
	bad := enriched(domain.MoneyFromFloat(0, "EUR"))
	bad.InvoiceNumber = 9999

	_, err := payment.GeneratePAIN001([]domain.EnrichedInvoice{good, bad}, debtor(), "MSG-1", time.Now())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "9999")
}

func TestGeneratePAIN001_ordinaryAmountsStillBuild(t *testing.T) {
	xml, err := payment.GeneratePAIN001(
		[]domain.EnrichedInvoice{enriched(domain.MoneyFromFloat(2450, "EUR"))},
		debtor(), "MSG-1", time.Now())

	require.NoError(t, err)
	assert.Contains(t, string(xml), "2450.00")
}
