package ingest_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mathiasb/cobalt-dingo/internal/extract"
	"github.com/mathiasb/cobalt-dingo/internal/ingest"
)

func edit(t *testing.T, doc []byte, old, repl string) []byte {
	t.Helper()
	s := string(doc)
	require.Contains(t, s, old, "the target text moved; update the test")
	return []byte(strings.Replace(s, old, repl, 1))
}

func TestParseUBL_ReadsTheFieldsOfAnInvoiceWithMixedVATRates(t *testing.T) {
	inv, err := ingest.ParseUBL(fixture(t, "ubl-invoice-mixed-vat.xml"))

	require.NoError(t, err)
	assert.Equal(t, extract.Invoice{
		Supplier: "Östersunds Hotell AB", InvoiceNumber: "91260121", InvoiceDate: "2026-11-02", DueDate: "2026-12-02",
		Currency: "SEK", NetTotal: 401155, NetKnown: true, VATAmount: 61075, Total: 462230,
	}, inv)
	assert.True(t, inv.Reconciled())
}

func TestParseUBL_ReadsAForeignCurrencyInvoiceWithNoVAT(t *testing.T) {
	inv, err := ingest.ParseUBL(fixture(t, "ubl-invoice-usd.xml"))

	require.NoError(t, err)
	assert.Equal(t, "Acme Cloud Inc", inv.Supplier)
	assert.Equal(t, "USD", inv.Currency)
	assert.Equal(t, int64(48363), inv.Total)
	assert.Equal(t, int64(0), inv.VATAmount)
	assert.True(t, inv.Reconciled())
}

// Downstream, a negative total is what makes an entry refuse with "credit notes
// are not supported". UBL writes a credit note's amounts as positive numbers and
// says so by its document type, so the sign is applied here.
func TestParseUBL_ASignedCreditNoteHasNegativeAmounts(t *testing.T) {
	inv, err := ingest.ParseUBL(fixture(t, "ubl-credit-note.xml"))

	require.NoError(t, err)
	assert.Equal(t, int64(-13295), inv.NetTotal)
	assert.Equal(t, int64(-13295), inv.Total)
	assert.Equal(t, int64(0), inv.VATAmount)
	assert.Empty(t, inv.DueDate, "a credit note states no due date")
	assert.True(t, inv.Reconciled(), "net + VAT = total holds with all three negative")
}

// The amount to pay is PayableAmount: it includes a rounding line, which
// TaxInclusiveAmount does not. The gap is what Reconciled's tolerance absorbs.
func TestParseUBL_TheTotalIsThePayableAmountNotTheTaxInclusiveOne(t *testing.T) {
	doc := fixture(t, "ubl-invoice-mixed-vat.xml")
	doc = edit(t, doc, `<cbc:PayableAmount currencyID="SEK">4622.30</cbc:PayableAmount>`, `<cbc:PayableRoundingAmount currencyID="SEK">-0.30</cbc:PayableRoundingAmount><cbc:PayableAmount currencyID="SEK">4622.00</cbc:PayableAmount>`)

	inv, err := ingest.ParseUBL(doc)

	require.NoError(t, err)
	assert.Equal(t, int64(462200), inv.Total)
	assert.True(t, inv.Reconciled(), "a 30 öre rounding gap is inside the tolerance")
}

func TestParseUBL_AWrongTotalIsReadAsWrittenAndFailsReconciliation(t *testing.T) {
	doc := edit(t, fixture(t, "ubl-invoice-mixed-vat.xml"), `<cbc:PayableAmount currencyID="SEK">4622.30</cbc:PayableAmount>`, `<cbc:PayableAmount currencyID="SEK">5622.30</cbc:PayableAmount>`)

	inv, err := ingest.ParseUBL(doc)

	require.NoError(t, err)
	assert.False(t, inv.Reconciled(), "the parser does not repair a total; the cross-check refuses it")
	assert.NotEmpty(t, inv.Warnings())
}

// The VAT amount is the TaxTotal in the document currency. A second TaxTotal in
// the accounting currency (Peppol allows one) must not be summed in or taken.
func TestParseUBL_TakesTheTaxTotalInTheDocumentCurrency(t *testing.T) {
	doc := edit(t, fixture(t, "ubl-invoice-mixed-vat.xml"), `<cac:TaxTotal>`, `<cac:TaxTotal><cbc:TaxAmount currencyID="EUR">55.00</cbc:TaxAmount></cac:TaxTotal><cac:TaxTotal>`)

	inv, err := ingest.ParseUBL(doc)

	require.NoError(t, err)
	assert.Equal(t, int64(61075), inv.VATAmount)
}

func TestParseUBL_RefusesADocumentThatIsHostileOrMalformed(t *testing.T) {
	base := fixture(t, "ubl-invoice-mixed-vat.xml")
	deep := []byte(`<Invoice xmlns="urn:oasis:names:specification:ubl:schema:xsd:Invoice-2">` + strings.Repeat("<a>", 200) + strings.Repeat("</a>", 200) + `</Invoice>`)
	huge := append(append([]byte{}, base[:len(base)-len("</Invoice>")]...), []byte("<!-- "+strings.Repeat("x", ingest.MaxXMLBytes)+" --></Invoice>")...)
	tests := []struct {
		name string
		doc  []byte
		want string
	}{
		{"a DOCTYPE with an external entity", edit(t, base, `<Invoice `, `<!DOCTYPE Invoice [<!ENTITY x SYSTEM "file:///etc/passwd">]><Invoice `), "DOCTYPE"},
		{"a DOCTYPE with an internal entity", edit(t, base, `<Invoice `, `<!DOCTYPE Invoice [<!ENTITY a "aaaa">]><Invoice `), "DOCTYPE"},
		{"nesting past the depth limit", deep, "deep"},
		{"more bytes than the size limit", huge, "larger"},
		{"not xml at all", []byte("hello"), "XML"},
		{"the wrong root element", []byte(`<Order xmlns="urn:oasis:names:specification:ubl:schema:xsd:Order-2"/>`), "not a UBL"},
		{"no invoice number", edit(t, base, `<cbc:ID>91260121</cbc:ID>`, ``), "ID"},
		{"no issue date", edit(t, base, `<cbc:IssueDate>2026-11-02</cbc:IssueDate>`, ``), "IssueDate"},
		{"an impossible issue date", edit(t, base, `2026-11-02</cbc:IssueDate>`, `2026-13-45</cbc:IssueDate>`), "IssueDate"},
		{"an impossible due date", edit(t, base, `2026-12-02</cbc:DueDate>`, `2026-02-30</cbc:DueDate>`), "DueDate"},
		{"a currency that is not an ISO code", edit(t, base, `<cbc:DocumentCurrencyCode>SEK`, `<cbc:DocumentCurrencyCode>kr`), "currency"},
		{"no payable amount", edit(t, base, `<cbc:PayableAmount currencyID="SEK">4622.30</cbc:PayableAmount>`, ``), "PayableAmount"},
		{"an amount that is not a number", edit(t, base, `4622.30</cbc:PayableAmount>`, `1e9</cbc:PayableAmount>`), "PayableAmount"},
		{"an amount with three decimals", edit(t, base, `4622.30</cbc:PayableAmount>`, `4622.305</cbc:PayableAmount>`), "PayableAmount"},
		{"an amount above the limit", edit(t, base, `4622.30</cbc:PayableAmount>`, `99999999999.99</cbc:PayableAmount>`), "PayableAmount"},
		{"a negative amount in an invoice", edit(t, base, `4622.30</cbc:PayableAmount>`, `-4622.30</cbc:PayableAmount>`), "PayableAmount"},
		{"no tax total in the document currency", edit(t, base, `<cac:TaxTotal><cbc:TaxAmount currencyID="SEK">`, `<cac:TaxTotal><cbc:TaxAmount currencyID="EUR">`), "TaxTotal"},
		{"a supplier name past the length limit", edit(t, base, `<cbc:RegistrationName>Östersunds Hotell AB`, `<cbc:RegistrationName>`+strings.Repeat("x", 300)), "supplier"},
		{"a newline in the supplier name", edit(t, base, `<cbc:RegistrationName>Östersunds Hotell AB`, "<cbc:RegistrationName>Hotell&#10;AB"), "supplier"},
		{"a raw control byte anywhere", edit(t, base, `<cbc:BuyerReference>`, "<cbc:BuyerReference>\x07"), "XML"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ingest.ParseUBL(tt.doc)

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.want)
		})
	}
}
