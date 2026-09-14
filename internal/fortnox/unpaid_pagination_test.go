package fortnox

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Both unpaid-invoice fetches read page one only. Fortnox paginates at 100, so
// a company with more than 100 open invoices would have silently under-
// reported its obligations — and the defect is invisible until it matters,
// because today both ledgers are empty (#91).
//
// Same class as the voucher list, which returned exactly 100 of 405 and
// produced a confident wrong answer about account 1930 (#90).
func TestUnpaidSupplierInvoices_readsEveryPage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page := r.URL.Query().Get("page")
		if page == "" {
			page = "1"
		}
		assert.Equal(t, "unpaid", r.URL.Query().Get("filter"), "the filter must survive pagination")
		_, _ = fmt.Fprintf(w, `{"MetaInformation":{"@CurrentPage":%s,"@TotalPages":3,"@TotalResources":3},
			"SupplierInvoices":[{"GivenNumber":"%s","SupplierNumber":"1","SupplierName":"Lev",
			"Currency":"SEK","Total":"100.00","Balance":"100.00","DueDate":"2026-10-01"}]}`, page, page)
	}))
	defer srv.Close()

	invoices, err := NewClient(srv.URL, "tok", true).UnpaidSupplierInvoices()
	require.NoError(t, err)
	assert.Len(t, invoices, 3, "one invoice from each of three pages")
}

func TestUnpaidCustomerInvoices_readsEveryPage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page := r.URL.Query().Get("page")
		if page == "" {
			page = "1"
		}
		assert.Equal(t, "unpaid", r.URL.Query().Get("filter"))
		_, _ = fmt.Fprintf(w, `{"MetaInformation":{"@CurrentPage":%s,"@TotalPages":2,"@TotalResources":2},
			"Invoices":[{"DocumentNumber":%s,"CustomerName":"Kund","Total":500.0,"Balance":500.0,
			"DueDate":"2026-10-01"}]}`, page, page)
	}))
	defer srv.Close()

	invoices, err := NewClient(srv.URL, "tok", true).UnpaidCustomerInvoices()
	require.NoError(t, err)
	assert.Len(t, invoices, 2)
}
