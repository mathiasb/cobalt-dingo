package fortnox

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// VoucherTotal is the cheap completeness check behind the voucher cache
// (ADR-0006): one request, reading Fortnox's own count for the year.
func TestVoucherTotal_readsTotalResourcesInOneRequest(t *testing.T) {
	var requests int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		_, _ = w.Write([]byte(`{"MetaInformation":{"@CurrentPage":1,"@TotalPages":5,"@TotalResources":405},"Vouchers":[]}`))
	}))
	defer srv.Close()

	total, err := NewClient(srv.URL, "tok", true).VoucherTotal(2)
	require.NoError(t, err)
	require.Equal(t, 405, total)

	// The whole value of this call is that it costs one request regardless of
	// how many vouchers the year holds. Paginating it would make the integrity
	// check as expensive as the fetch it guards.
	require.Equal(t, 1, requests, "VoucherTotal must not paginate")
}

// A year really can hold zero vouchers, and that is a legitimate verified
// answer — distinct from the case below.
func TestVoucherTotal_zeroIsAValidAnswer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"MetaInformation":{"@CurrentPage":1,"@TotalPages":0,"@TotalResources":0},"Vouchers":[]}`))
	}))
	defer srv.Close()

	total, err := NewClient(srv.URL, "tok", true).VoucherTotal(2)
	require.NoError(t, err)
	require.Equal(t, 0, total)
}

// The important one. A response with no MetaInformation at all decodes to
// TotalResources == 0, which is indistinguishable from "this year is empty" —
// and VoucherCacheState.Assess would then read an empty cache agreeing with an
// empty remote as FRESH. That is a measurement failure being served as a
// verified figure, so it has to be an error, not a count.
func TestVoucherTotal_absentMetaInformationIsAnErrorNotZero(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"Vouchers":[]}`))
	}))
	defer srv.Close()

	_, err := NewClient(srv.URL, "tok", true).VoucherTotal(2)
	require.Error(t, err)
	require.Contains(t, strings.ToLower(err.Error()), "metainformation")
}

func TestVoucherTotal_requestsTheFinancialYear(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		_, _ = w.Write([]byte(`{"MetaInformation":{"@TotalResources":1},"Vouchers":[]}`))
	}))
	defer srv.Close()

	_, err := NewClient(srv.URL, "tok", true).VoucherTotal(7)
	require.NoError(t, err)
	require.Contains(t, gotQuery, "financialyear=7")
}
