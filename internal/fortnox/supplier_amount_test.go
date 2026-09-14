package fortnox

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The live shape, from cmd/fortnox-shape against production 2026-09-14:
// Total and Balance are STRINGS, the document identifier is GivenNumber, and
// TotalInvoiceCurrency — which this code used to read as the amount — is not
// sent at all. Every supplier invoice therefore had Amount SEK 0.00, including
// on the path that builds PAIN.001 payment instructions.
func TestSupplierInvoices_amountComesFromTotalNotAFieldFortnoxNeverSends(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"MetaInformation":{"@CurrentPage":1,"@TotalPages":1,"@TotalResources":1},
			"SupplierInvoices":[{
				"GivenNumber":"9001",
				"InvoiceNumber":"SUPP-REF-77",
				"SupplierNumber":"42",
				"SupplierName":"Lev AB",
				"Currency":"SEK",
				"Total":"12500.50",
				"Balance":"7500.25",
				"DueDate":"2026-09-30",
				"Booked":true,
				"Cancelled":false
			}]}`))
	}))
	defer srv.Close()

	invoices, err := NewClient(srv.URL, "tok", true).UnpaidSupplierInvoices()
	require.NoError(t, err)
	require.Len(t, invoices, 1)
	inv := invoices[0]

	assert.Equal(t, int64(1250050), inv.Amount.MinorUnits, "Total, parsed from a string")
	assert.Equal(t, "SEK", inv.Amount.Currency)
	assert.Equal(t, int64(750025), inv.Balance.MinorUnits, "Balance is what remains owing")
	assert.Equal(t, "SEK", inv.Balance.Currency)

	// Identity is Fortnox's GivenNumber — the value its own URLs take.
	// InvoiceNumber is the SUPPLIER's reference and need not be numeric.
	assert.Equal(t, 9001, inv.InvoiceNumber)
	assert.Equal(t, "SUPP-REF-77", inv.SupplierReference)
	assert.Equal(t, 42, inv.SupplierNumber)
	assert.True(t, inv.Booked)
	assert.False(t, inv.Cancelled)
}

// A non-numeric supplier reference must not break the fetch. Parsing it as an
// int was only survivable while it happened to look like a number.
func TestSupplierInvoices_nonNumericSupplierReferenceIsFine(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"MetaInformation":{"@CurrentPage":1,"@TotalPages":1,"@TotalResources":1},
			"SupplierInvoices":[{"GivenNumber":"12","InvoiceNumber":"2026/A-114","SupplierName":"Lev",
			"Currency":"SEK","Total":"100.00","Balance":"100.00"}]}`))
	}))
	defer srv.Close()

	invoices, err := NewClient(srv.URL, "tok", true).UnpaidSupplierInvoices()
	require.NoError(t, err)
	require.Len(t, invoices, 1)
	assert.Equal(t, 12, invoices[0].InvoiceNumber)
	assert.Equal(t, "2026/A-114", invoices[0].SupplierReference)
	assert.Equal(t, int64(10000), invoices[0].Amount.MinorUnits)
}
