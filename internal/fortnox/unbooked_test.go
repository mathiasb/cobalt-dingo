package fortnox

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// #91 resolved to unbooked invoices: 1 supplier, 1 customer, invisible to both
// the unpaid filter and the general ledger. A count is not actionable — the
// next question is always "which one, for how much".
func TestUnbookedSupplierInvoices_fetchesTheUnbookedFilter(t *testing.T) {
	var gotFilter string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotFilter = r.URL.Query().Get("filter")
		_, _ = w.Write([]byte(`{"MetaInformation":{"@CurrentPage":1,"@TotalPages":1,"@TotalResources":1},
			"SupplierInvoices":[{"GivenNumber":"9001","InvoiceNumber":"LEV-1","SupplierNumber":"42",
			"SupplierName":"Lev AB","Currency":"SEK","Total":"12500.00","Balance":"12500.00",
			"DueDate":"2026-09-30"}]}`))
	}))
	defer srv.Close()

	invoices, err := NewClient(srv.URL, "tok", true).UnbookedSupplierInvoices()
	require.NoError(t, err)
	assert.Equal(t, "unbooked", gotFilter)
	require.Len(t, invoices, 1)
	assert.Equal(t, 9001, invoices[0].InvoiceNumber)
	assert.Equal(t, "Lev AB", invoices[0].SupplierName)
	assert.Equal(t, int64(1250000), invoices[0].Amount.MinorUnits)
	assert.Equal(t, "2026-09-30", invoices[0].DueDate)
}

func TestUnbookedCustomerInvoices_fetchesTheUnbookedFilter(t *testing.T) {
	var gotFilter string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotFilter = r.URL.Query().Get("filter")
		_, _ = w.Write([]byte(`{"MetaInformation":{"@CurrentPage":1,"@TotalPages":1,"@TotalResources":1},
			"Invoices":[{"DocumentNumber":31,"CustomerName":"Kund AB","Total":18750.0,"Balance":18750.0,
			"DueDate":"2026-10-15","Booked":false}]}`))
	}))
	defer srv.Close()

	invoices, err := NewClient(srv.URL, "tok", true).UnbookedCustomerInvoices()
	require.NoError(t, err)
	assert.Equal(t, "unbooked", gotFilter)
	require.Len(t, invoices, 1)
	assert.Equal(t, 31, int(invoices[0].DocumentNumber))
	assert.Equal(t, "Kund AB", invoices[0].CustomerName)
}

// Both must paginate, for the same reason the unpaid fetches now do.
func TestUnbookedSupplierInvoices_readsEveryPage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page := r.URL.Query().Get("page")
		if page == "" {
			page = "1"
		}
		_, _ = w.Write([]byte(`{"MetaInformation":{"@CurrentPage":` + page + `,"@TotalPages":2,"@TotalResources":2},
			"SupplierInvoices":[{"GivenNumber":"1","SupplierName":"Lev","Currency":"SEK","Total":"1.00","Balance":"1.00"}]}`))
	}))
	defer srv.Close()

	invoices, err := NewClient(srv.URL, "tok", true).UnbookedSupplierInvoices()
	require.NoError(t, err)
	assert.Len(t, invoices, 2)
}
