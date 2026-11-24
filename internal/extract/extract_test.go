package extract_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mathiasb/cobalt-dingo/internal/extract"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const goodReply = `{"supplier":"Fjällresor AB","invoice_number":"56260004","invoice_date":"2026-11-11",` +
	`"due_date":"2026-12-11","currency":"SEK","total":"3592.39","vat_amount":"203.34"}`

// stub serves an Ollama-style /api/chat reply whose message content is content.
func stub(t *testing.T, content string, captured *map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if captured != nil {
			require.Equal(t, "/api/chat", r.URL.Path)
			require.Equal(t, http.MethodPost, r.Method)
			require.NoError(t, json.NewDecoder(r.Body).Decode(captured))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"message": map[string]any{"content": content}})
	}))
}

func TestExtract_SendsTheImageAndAJSONFormatRequest(t *testing.T) {
	var got map[string]any
	srv := stub(t, goodReply, &got)
	defer srv.Close()
	c := &extract.Client{BaseURL: srv.URL, Model: "qwen2.5vl:7b"}

	_, err := c.Extract(context.Background(), []byte("PNGBYTES"))

	require.NoError(t, err)
	assert.Equal(t, "qwen2.5vl:7b", got["model"])
	assert.Equal(t, "json", got["format"])
	assert.Equal(t, false, got["stream"])
	msgs := got["messages"].([]any)
	imgs := msgs[0].(map[string]any)["images"].([]any)
	assert.Equal(t, base64.StdEncoding.EncodeToString([]byte("PNGBYTES")), imgs[0])
}

func TestExtract_ParsesAGoodReplyIntoTypedFieldsWithMinorUnits(t *testing.T) {
	srv := stub(t, goodReply, nil)
	defer srv.Close()
	c := &extract.Client{BaseURL: srv.URL, Model: "m"}

	inv, err := c.Extract(context.Background(), []byte("x"))

	require.NoError(t, err)
	assert.Equal(t, "Fjällresor AB", inv.Supplier)
	assert.Equal(t, "56260004", inv.InvoiceNumber)
	assert.Equal(t, "2026-11-11", inv.InvoiceDate)
	assert.Equal(t, "2026-12-11", inv.DueDate)
	assert.Equal(t, "SEK", inv.Currency)
	assert.Equal(t, int64(359239), inv.Total)
	assert.Equal(t, int64(20334), inv.VATAmount)
}

func TestExtract_RejectsAReplyThatIsNotUsableAndNamesTheField(t *testing.T) {
	tests := []struct {
		name, reply, wantField string
	}{
		{"not json", `sorry, I cannot read this`, "json"},
		{"supplier null", `{"supplier":null,"invoice_number":"1","invoice_date":"2026-11-11","due_date":"2026-12-11","currency":"SEK","total":"1.00","vat_amount":"0.00"}`, "supplier"},
		{"invoice number missing", `{"supplier":"A","invoice_date":"2026-11-11","due_date":"2026-12-11","currency":"SEK","total":"1.00","vat_amount":"0.00"}`, "invoice_number"},
		{"date not ISO", `{"supplier":"A","invoice_number":"1","invoice_date":"11/11/2026","due_date":"2026-12-11","currency":"SEK","total":"1.00","vat_amount":"0.00"}`, "invoice_date"},
		{"currency not a code", `{"supplier":"A","invoice_number":"1","invoice_date":"2026-11-11","due_date":"2026-12-11","currency":"kronor","total":"1.00","vat_amount":"0.00"}`, "currency"},
		{"currency lower case", `{"supplier":"A","invoice_number":"1","invoice_date":"2026-11-11","due_date":"2026-12-11","currency":"sek","total":"1.00","vat_amount":"0.00"}`, "currency"},
		{"total not a number", `{"supplier":"A","invoice_number":"1","invoice_date":"2026-11-11","due_date":"2026-12-11","currency":"SEK","total":"abc","vat_amount":"0.00"}`, "total"},
		{"total with three decimals", `{"supplier":"A","invoice_number":"1","invoice_date":"2026-11-11","due_date":"2026-12-11","currency":"SEK","total":"1.234","vat_amount":"0.00"}`, "total"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := stub(t, tt.reply, nil)
			defer srv.Close()
			c := &extract.Client{BaseURL: srv.URL, Model: "m"}

			inv, err := c.Extract(context.Background(), []byte("x"))

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantField)
			assert.Equal(t, extract.Invoice{}, inv, "no partial data passed off as a result")
		})
	}
}

func TestParseAmount_ReadsDecimalsIntoMinorUnitsWithoutFloats(t *testing.T) {
	tests := []struct {
		in   string
		want int64
	}{
		{"3592.39", 359239},
		{"3592,39", 359239},
		{"3 592.39", 359239},
		{"3592", 359200},
		{"0.05", 5},
		{"17.5", 1750},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := extract.ParseAmount(tt.in)

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestExtract_AcceptsAnAmountSentAsAJSONNumber(t *testing.T) {
	srv := stub(t, `{"supplier":"A","invoice_number":"1","invoice_date":"2026-11-11","due_date":"2026-12-11","currency":"SEK","total":3592.39,"vat_amount":203.34}`, nil)
	defer srv.Close()
	c := &extract.Client{BaseURL: srv.URL, Model: "m"}

	inv, err := c.Extract(context.Background(), []byte("x"))

	require.NoError(t, err)
	assert.Equal(t, int64(359239), inv.Total)
}

func TestExtract_FailsWhenTheModelServerIsUnreachable(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()
	c := &extract.Client{BaseURL: url, Model: "m"}

	_, err := c.Extract(context.Background(), []byte("x"))

	require.Error(t, err)
}

func TestExtract_FailsOnANon200Response(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "model not found", http.StatusNotFound)
	}))
	defer srv.Close()
	c := &extract.Client{BaseURL: srv.URL, Model: "missing"}

	_, err := c.Extract(context.Background(), []byte("x"))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "404")
}

// Real invoices often carry no due date (10 of 10 in the katanaml sample). The
// booking proposal does not need one, so absence is not an error; a due date
// that IS present and malformed still is.
func TestExtract_AcceptsAReplyWithNoDueDate(t *testing.T) {
	srv := stub(t, `{"supplier":"Smith Ltd","invoice_number":"26343874","invoice_date":"2015-09-18","due_date":null,"currency":"USD","total":"3490.29","vat_amount":"317.30"}`, nil)
	defer srv.Close()
	c := &extract.Client{BaseURL: srv.URL, Model: "m"}

	inv, err := c.Extract(context.Background(), []byte("x"))

	require.NoError(t, err)
	assert.Equal(t, "", inv.DueDate)
	assert.Equal(t, int64(349029), inv.Total)
}

func TestExtract_StillRejectsAMalformedDueDateThatIsPresent(t *testing.T) {
	srv := stub(t, `{"supplier":"A","invoice_number":"1","invoice_date":"2026-11-11","due_date":"next month","currency":"SEK","total":"1.00","vat_amount":"0.00"}`, nil)
	defer srv.Close()
	c := &extract.Client{BaseURL: srv.URL, Model: "m"}

	_, err := c.Extract(context.Background(), []byte("x"))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "due_date")
}
