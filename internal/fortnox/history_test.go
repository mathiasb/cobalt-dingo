package fortnox

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListSupplierInvoiceHeads_ReadsEveryPageAndDecodesTheSupplierAndVoucherLink(t *testing.T) {
	var queries []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/3/supplierinvoices", r.URL.Path)
		require.Equal(t, http.MethodGet, r.Method)
		queries = append(queries, r.URL.RawQuery)
		switch r.URL.Query().Get("page") {
		case "2":
			_, _ = fmt.Fprint(w, `{"MetaInformation":{"@CurrentPage":2,"@TotalPages":2,"@TotalResources":2},"SupplierInvoices":[`+
				`{"GivenNumber":"8","SupplierNumber":1002,"SupplierName":"Beta Ltd","InvoiceDate":"2026-02-03","Currency":"EUR","Booked":false,"Cancelled":true,"VoucherSeries":"","VoucherNumber":0,"VoucherYear":""}]}`)
		default:
			_, _ = fmt.Fprint(w, `{"MetaInformation":{"@CurrentPage":1,"@TotalPages":2,"@TotalResources":2},"SupplierInvoices":[`+
				`{"GivenNumber":"7","SupplierNumber":"1001","SupplierName":"Alfa AB","InvoiceDate":"2026-01-05","Currency":"SEK","Booked":true,"Cancelled":false,"VoucherSeries":"A","VoucherNumber":41,"VoucherYear":"2026"}]}`)
		}
	}))
	defer srv.Close()

	got, err := NewClient(srv.URL, "tok", true).ListSupplierInvoiceHeads("2025-01-01", "2026-12-31")

	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, SupplierInvoiceHead{GivenNumber: "7", SupplierNumber: 1001, SupplierName: "Alfa AB", InvoiceDate: "2026-01-05", Currency: "SEK", Booked: true, VoucherSeries: "A", VoucherNumber: 41, VoucherYear: 2026}, got[0])
	assert.Equal(t, 1002, got[1].SupplierNumber, "a bare number decodes as well as a quoted one")
	assert.True(t, got[1].Cancelled)
	assert.Equal(t, 0, got[1].VoucherNumber, "an unbooked invoice has no voucher link")
	assert.Contains(t, queries[0], "fromdate=2025-01-01")
	assert.Contains(t, queries[0], "todate=2026-12-31")
}

func TestGetSupplierInvoiceRows_DecodesAccountsAndAmountsFromTheRows(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/3/supplierinvoices/7", r.URL.Path)
		_, _ = fmt.Fprint(w, `{"SupplierInvoice":{"GivenNumber":"7","SupplierInvoiceRows":[`+
			`{"Account":5460,"Debit":1000.5,"Credit":0,"CostCenter":"","Project":"P1"},`+
			`{"Account":"2641","Debit":"250.13","Credit":0},`+
			`{"Account":2440,"Debit":0,"Credit":1250.63}]}}`)
	}))
	defer srv.Close()

	rows, err := NewClient(srv.URL, "tok", true).GetSupplierInvoiceRows("7")

	require.NoError(t, err)
	require.Len(t, rows, 3)
	assert.Equal(t, SupplierInvoiceRowDetail{Account: 5460, Debit: 1000.5, Project: "P1"}, rows[0])
	assert.Equal(t, 2641, rows[1].Account, "a quoted account decodes")
	assert.InDelta(t, 250.13, rows[1].Debit, 0.0001, "a quoted amount decodes")
	assert.InDelta(t, 1250.63, rows[2].Credit, 0.0001)
}

func TestGetSupplierInvoiceRows_FailsOnANon200AndOnAnUndecodableBody(t *testing.T) {
	notFound := httptest.NewServer(http.NotFoundHandler())
	defer notFound.Close()
	garbled := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = fmt.Fprint(w, `{"SupplierInvoice":"nope"}`) }))
	defer garbled.Close()

	_, err404 := NewClient(notFound.URL, "tok", true).GetSupplierInvoiceRows("7")
	_, errBad := NewClient(garbled.URL, "tok", true).GetSupplierInvoiceRows("7")

	require.Error(t, err404)
	assert.Contains(t, err404.Error(), "404")
	require.Error(t, errBad)
}

// The identity probe exists to answer "which identifiers are present", so the
// type it returns carries presence flags and no name or number a person could
// read back.
func TestListSupplierIdentities_FlagsWhichIdentifiersArePresentAndCarriesNoValues(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/3/suppliers", r.URL.Path)
		_, _ = fmt.Fprint(w, `{"MetaInformation":{"@CurrentPage":1,"@TotalPages":1,"@TotalResources":2},"Suppliers":[`+
			`{"SupplierNumber":"1001","Name":"Alfa AB","OrganisationNumber":"556123-4567","VATNumber":"SE556123456701","BankAccountNumber":"123-456","IBAN":"SE35500000000549100000","PreDefinedAccount":"6540","CountryCode":"SE"},`+
			`{"SupplierNumber":1002,"Name":"Beta Ltd","OrganisationNumber":"","VATNumber":"","BankAccountNumber":"","IBAN":"","PreDefinedAccount":"","CountryCode":"IE"}]}`)
	}))
	defer srv.Close()

	got, err := NewClient(srv.URL, "tok", true).ListSupplierIdentities()

	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, SupplierIdentity{SupplierNumber: 1001, HasOrganisationNumber: true, HasVATNumber: true, HasBankAccount: true, HasIBAN: true, PreDefinedAccount: 6540, CountryCode: "SE"}, got[0])
	assert.Equal(t, SupplierIdentity{SupplierNumber: 1002, CountryCode: "IE"}, got[1])
}
