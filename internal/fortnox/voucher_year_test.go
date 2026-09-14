package fortnox

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A voucher is identified by (financial year, series, number) — numbers
// restart per series each year. Omitting the year lets Fortnox answer about a
// voucher we did not ask for.
//
// Measured on live production 2026-09-14: without financialyear, four of 405
// vouchers came back carrying rows that did not belong to them — a complete,
// balanced voucher plus a partial second one. M/2 balanced on its first six
// rows and then credited account 2650 a second time, putting the company's
// whole trial balance SEK 122,881.80 out.
func TestGetVoucher_sendsTheFinancialYear(t *testing.T) {
	var gotPath, gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotQuery = r.URL.Path, r.URL.RawQuery
		_, _ = w.Write([]byte(`{"Voucher":{"VoucherSeries":"M","VoucherNumber":2,"VoucherRows":[]}}`))
	}))
	defer srv.Close()

	_, err := NewClient(srv.URL, "tok", true).GetVoucher(3, "M", 2)
	require.NoError(t, err)
	assert.Equal(t, "/3/vouchers/M/2", gotPath)
	assert.Contains(t, gotQuery, "financialyear=3")
}

// The detail fetch inside ListVouchersWithRows must carry the same year it was
// asked about. This is where the live defect actually bit.
func TestListVouchersWithRows_detailFetchCarriesTheYear(t *testing.T) {
	var detailQueries []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/3/vouchers" {
			_, _ = w.Write([]byte(`{"MetaInformation":{"@CurrentPage":1,"@TotalPages":1,"@TotalResources":1},
				"Vouchers":[{"VoucherSeries":"M","VoucherNumber":2,"TransactionDate":"2026-06-30","VoucherRows":[]}]}`))
			return
		}
		detailQueries = append(detailQueries, r.URL.RawQuery)
		_, _ = w.Write([]byte(`{"Voucher":{"VoucherSeries":"M","VoucherNumber":2,
			"VoucherRows":[{"Account":2611,"Debit":143750,"Credit":0}]}}`))
	}))
	defer srv.Close()

	vouchers, err := NewClient(srv.URL, "tok", true).ListVouchersWithRows(3)
	require.NoError(t, err)
	require.Len(t, vouchers, 1)
	require.Len(t, detailQueries, 1)
	assert.Contains(t, detailQueries[0], "financialyear=3")
}
