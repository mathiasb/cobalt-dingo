package apishape_test

import (
	"testing"

	"github.com/mathiasb/cobalt-dingo/internal/apishape"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The point of this package: compare what Fortnox ACTUALLY sends against what
// the vendored spec declares, in both directions. The spec is a document, not
// the API — it omits TotalInvoiceCurrency, which our supplier invoice struct
// has been reading for months (#92).
func TestRecordKeys_listsTheKeysOfTheFirstRecord(t *testing.T) {
	body := []byte(`{"MetaInformation":{"@TotalResources":1},
		"SupplierInvoices":[{"GivenNumber":"9001","Total":"12500.00","Booked":false}]}`)

	keys, err := apishape.RecordKeys(body, "SupplierInvoices")
	require.NoError(t, err)
	assert.Equal(t, []string{"Booked", "GivenNumber", "Total"}, keys, "sorted, so two runs are diffable")
}

// Types matter as much as names here: Fortnox returns Total as a STRING, and a
// float64 field decodes that to zero without erroring... which is precisely
// how an invoice amount becomes SEK 0.00 silently.
func TestRecordTypes_reportsTheJSONTypeOfEachKey(t *testing.T) {
	body := []byte(`{"Invoices":[{"DocumentNumber":"31","Total":18750.0,"Booked":false,"OCR":null}]}`)

	types, err := apishape.RecordTypes(body, "Invoices")
	require.NoError(t, err)
	assert.Equal(t, "string", types["DocumentNumber"])
	assert.Equal(t, "number", types["Total"])
	assert.Equal(t, "bool", types["Booked"])
	assert.Equal(t, "null", types["OCR"])
}

// An empty collection cannot describe a shape, and saying so beats returning
// an empty key list that reads like "this record has no fields".
func TestRecordKeys_emptyCollectionIsAnError(t *testing.T) {
	_, err := apishape.RecordKeys([]byte(`{"SupplierInvoices":[]}`), "SupplierInvoices")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no records")
}

func TestRecordKeys_missingCollectionNamesWhatWasThere(t *testing.T) {
	_, err := apishape.RecordKeys([]byte(`{"Invoices":[{"a":1}]}`), "SupplierInvoices")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Invoices")
}

// The comparison that answers #92: which declared fields do we not read, and
// which fields do we read that are not declared.
func TestCompare_reportsBothDirections(t *testing.T) {
	live := []string{"GivenNumber", "Total", "Booked", "Balance"}
	ours := []string{"InvoiceNumber", "Total", "TotalInvoiceCurrency"}

	d := apishape.Compare(live, ours)

	assert.Equal(t, []string{"Balance", "Booked", "GivenNumber"}, d.LiveNotRead,
		"fields Fortnox sends that we ignore")
	assert.Equal(t, []string{"InvoiceNumber", "TotalInvoiceCurrency"}, d.ReadNotLive,
		"fields we decode that Fortnox did not send — these are silently zero")
}
