package fortnox

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The defect this exists for (#89): GET /3/vouchers declares VoucherRows and
// returns them empty. Measured on live production — 405 vouchers, 0 with rows —
// so every row-level analysis returned nothing and looked like an answer.
//
// Rows come from the per-voucher detail endpoint, which means one request per
// voucher. At the documented limit (300/min, 25 per 5s; our limiter uses 18)
// a 405-voucher year costs about two minutes. That is the price of correctness
// here and it belongs in a scheduled snapshot rather than an interactive call.
func TestListVouchersWithRows_fetchesRowsFromTheDetailEndpoint(t *testing.T) {
	var detailCalls []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// List: two vouchers, no rows — exactly what Fortnox really returns.
		if r.URL.Path == "/3/vouchers" {
			_, _ = w.Write([]byte(`{"MetaInformation":{"@CurrentPage":1,"@TotalPages":1,"@TotalResources":2},
			  "Vouchers":[
			    {"VoucherSeries":"A","VoucherNumber":1,"TransactionDate":"2026-03-01"},
			    {"VoucherSeries":"B","VoucherNumber":7,"TransactionDate":"2026-04-02"}]}`))
			return
		}

		// Detail: carries the rows.
		detailCalls = append(detailCalls, r.URL.Path)
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/3/vouchers/"), "/")
		_, _ = fmt.Fprintf(w, `{"Voucher":{"VoucherSeries":"%s","VoucherNumber":%s,
		  "TransactionDate":"2026-03-01",
		  "VoucherRows":[{"Account":1930,"Debit":100.0,"Credit":0},{"Account":2440,"Debit":0,"Credit":100.0}]}}`,
			parts[0], parts[1])
	}))
	defer srv.Close()

	got, err := NewClient(srv.URL, "tok", true).ListVouchersWithRows(3)
	require.NoError(t, err)
	require.Len(t, got, 2)

	for _, v := range got {
		assert.Lenf(t, v.VoucherRows, 2, "voucher %s%d must carry its rows", v.VoucherSeries, int(v.VoucherNumber))
	}
	assert.ElementsMatch(t, []string{"/3/vouchers/A/1", "/3/vouchers/B/7"}, detailCalls,
		"one detail request per voucher, addressed by series and number")
}

// A voucher whose detail cannot be read must not silently vanish from the
// result — that would reproduce #89 with extra steps, returning a short list
// that looks complete.
func TestListVouchersWithRows_failsRatherThanDroppingAVoucher(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/3/vouchers" {
			_, _ = w.Write([]byte(`{"MetaInformation":{"@CurrentPage":1,"@TotalPages":1,"@TotalResources":1},
			  "Vouchers":[{"VoucherSeries":"A","VoucherNumber":1,"TransactionDate":"2026-03-01"}]}`))
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	_, err := NewClient(srv.URL, "tok", true).ListVouchersWithRows(3)
	require.Error(t, err, "an unreadable voucher is an error, not an omission")
	assert.Contains(t, err.Error(), "A/1", "the error must name the voucher that failed")
}

// An empty year costs exactly one request: no list, no details.
func TestListVouchersWithRows_emptyYearMakesNoDetailCalls(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"MetaInformation":{"@CurrentPage":1,"@TotalPages":1,"@TotalResources":0},"Vouchers":[]}`))
	}))
	defer srv.Close()

	got, err := NewClient(srv.URL, "tok", true).ListVouchersWithRows(3)
	require.NoError(t, err)
	assert.Empty(t, got)
	assert.Equal(t, 1, calls)
}
