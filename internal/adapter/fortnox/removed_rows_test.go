package fortnox_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	adapterfortnox "github.com/mathiasb/cobalt-dingo/internal/adapter/fortnox"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Fortnox's VoucherRow carries a Removed flag (VoucherRowSingleItem in the
// OpenAPI spec) and the detail endpoint returns removed rows alongside live
// ones. We were not decoding it, so corrections were counted twice.
//
// Measured on live production 2026-09-14: four of 405 vouchers appeared not to
// balance, and their differences summed to exactly the company's whole trial
// balance discrepancy, SEK -122,881.80. A/144 carried a debit to 1930 with TWO
// matching credits — one of them removed.
//
// Fortnox will not accept an unbalanced voucher through its own UI, so a
// voucher that does not balance always means we are reading it wrong.
func TestVoucherSource_removedRowsAreNotCounted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasPrefix(r.URL.Path, "/3/vouchers/") {
			_, _ = w.Write([]byte(`{"Voucher":{"VoucherSeries":"A","VoucherNumber":144,
				"TransactionDate":"2026-01-31","Year":3,"VoucherRows":[
				{"Account":1930,"Debit":347.15,"Credit":0,"Removed":false},
				{"Account":1940,"Debit":0,"Credit":347.15,"Removed":false},
				{"Account":8310,"Debit":0,"Credit":347.15,"Removed":true}]}}`))
			return
		}
		_, _ = w.Write([]byte(`{"MetaInformation":{"@CurrentPage":1,"@TotalPages":1,"@TotalResources":1},
			"Vouchers":[{"VoucherSeries":"A","VoucherNumber":144,"TransactionDate":"2026-01-31","Year":3,"VoucherRows":[]}]}`))
	}))
	defer srv.Close()

	vouchers, err := adapterfortnox.NewVoucherSourceAdapter(srv.URL, newStubTokenStore()).
		FetchYear(context.Background(), "tenant-1", 3)

	require.NoError(t, err)
	require.Len(t, vouchers, 1)
	require.Len(t, vouchers[0].Rows, 2, "the removed row must not appear")

	var diff int64
	for _, r := range vouchers[0].Rows {
		diff += r.Debit.MinorUnits - r.Credit.MinorUnits
		assert.NotEqual(t, 8310, r.Account, "row 8310 was removed in Fortnox")
	}
	assert.Zero(t, diff, "the voucher balances once removed rows are excluded")
}
