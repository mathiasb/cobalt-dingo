package fortnox_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	adapterfortnox "github.com/mathiasb/cobalt-dingo/internal/adapter/fortnox"
	"github.com/mathiasb/cobalt-dingo/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The adapter must satisfy the port the cache policy is written against.
var _ domain.VoucherSource = (*adapterfortnox.VoucherSourceAdapter)(nil)

// voucherFixture serves Fortnox's real shape: the LIST endpoint returns
// vouchers with no rows, and rows arrive only from the per-voucher detail
// endpoint (#89, measured on live production 2026-09-14). A fixture that put
// rows in the list would let a broken adapter pass.
func voucherFixture(t *testing.T, total int) (*httptest.Server, *int) {
	t.Helper()
	detailCalls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasPrefix(r.URL.Path, "/3/vouchers/") {
			detailCalls++
			_, _ = w.Write([]byte(`{"Voucher":{"VoucherSeries":"A","VoucherNumber":1,"Description":"Bankavgift",
				"TransactionDate":"2026-03-01","Year":2,
				"VoucherRows":[{"Account":6570,"Debit":75.0,"Credit":0,"TransactionInformation":"avgift","CostCenter":"ADM","Project":"P1"},
				               {"Account":1930,"Debit":0,"Credit":75.0,"TransactionInformation":"avgift"}]}}`))
			return
		}
		_, _ = w.Write([]byte(`{"MetaInformation":{"@CurrentPage":1,"@TotalPages":1,"@TotalResources":` +
			strconv.Itoa(total) + `},"Vouchers":[{"VoucherSeries":"A","VoucherNumber":1,"Description":"Bankavgift",
			"TransactionDate":"2026-03-01","Year":2,"VoucherRows":[]}]}`))
	}))
	t.Cleanup(srv.Close)
	return srv, &detailCalls
}

func TestVoucherSource_RemoteTotalReadsFortnoxOwnCount(t *testing.T) {
	srv, detailCalls := voucherFixture(t, 405)

	src := adapterfortnox.NewVoucherSourceAdapter(srv.URL, newStubTokenStore())
	total, err := src.RemoteTotal(context.Background(), "tenant-1", 2)

	require.NoError(t, err)
	assert.Equal(t, 405, total)
	// The integrity check must not touch the detail endpoint — that is the
	// expensive path it exists to avoid.
	assert.Zero(t, *detailCalls)
}

// The defect this adapter would otherwise reintroduce: caching the LIST
// response stores vouchers with no rows, and the sync row then certifies them
// as complete. Every account, project and cost-centre figure derived from the
// cache would be zero and would look like an answer.
func TestVoucherSource_FetchYearReturnsRowsNotJustHeaders(t *testing.T) {
	srv, detailCalls := voucherFixture(t, 1)

	src := adapterfortnox.NewVoucherSourceAdapter(srv.URL, newStubTokenStore())
	vouchers, err := src.FetchYear(context.Background(), "tenant-1", 2)

	require.NoError(t, err)
	require.Len(t, vouchers, 1)
	require.Len(t, vouchers[0].Rows, 2, "rows must come from the detail endpoint")
	assert.Equal(t, 1, *detailCalls)

	assert.Equal(t, "A", vouchers[0].Series)
	assert.Equal(t, 1, vouchers[0].Number)
	assert.Equal(t, "2026-03-01", vouchers[0].TransactionDate)
	assert.Equal(t, 6570, vouchers[0].Rows[0].Account)
	assert.Equal(t, int64(7500), vouchers[0].Rows[0].Debit.MinorUnits)
	assert.Equal(t, "ADM", vouchers[0].Rows[0].CostCenter)
	assert.Equal(t, "P1", vouchers[0].Rows[0].Project)
	assert.Equal(t, 1930, vouchers[0].Rows[1].Account)
	assert.Equal(t, int64(7500), vouchers[0].Rows[1].Credit.MinorUnits)
}

// FetchYear must not filter by date. The cache stores whole financial years,
// because the year is the unit Fortnox reports a total for — a date-filtered
// set saved under the year's key would never match the remote count, and
// worse, could match it by coincidence.
func TestVoucherSource_FetchYearDoesNotFilterByDate(t *testing.T) {
	srv, _ := voucherFixture(t, 1)

	src := adapterfortnox.NewVoucherSourceAdapter(srv.URL, newStubTokenStore())
	vouchers, err := src.FetchYear(context.Background(), "tenant-1", 2)

	require.NoError(t, err)
	assert.Len(t, vouchers, 1)
}
