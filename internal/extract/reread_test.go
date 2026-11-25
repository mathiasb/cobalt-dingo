package extract_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/mathiasb/cobalt-dingo/internal/extract"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sequence serves one reply per call, in order, and records every request.
// The last reply repeats if the client calls more often than expected.
type sequence struct {
	srv      *httptest.Server
	mu       sync.Mutex
	requests []map[string]any
}

func newSequence(t *testing.T, replies ...string) *sequence {
	t.Helper()
	s := &sequence{}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		s.mu.Lock()
		i := len(s.requests)
		s.requests = append(s.requests, req)
		s.mu.Unlock()
		if i >= len(replies) {
			i = len(replies) - 1
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"message": map[string]any{"content": replies[i]}})
	}))
	t.Cleanup(s.srv.Close)
	return s
}

func (s *sequence) extract(t *testing.T) extract.Invoice {
	t.Helper()
	c := &extract.Client{BaseURL: s.srv.URL, Model: "m"}
	inv, err := c.Extract(context.Background(), []byte("IMG"))
	require.NoError(t, err)
	return inv
}

// The shape of the failure found on a real layout: the invoice prints a discount of
// 17.54 and a total of 1550.24 and no VAT. The model read the discount as VAT and
// made up a total that makes net + VAT = total hold.
const discountAsVAT = `{"supplier":"Mclean-Cochran","invoice_number":"INV1","invoice_date":"2022-05-19","due_date":null,` +
	`"currency":"USD","net_total":"1550.24","vat_amount":"17.54","total":"1567.78"}`

func TestExtract_ASecondReadOfTheTotalThatDisagreesLeavesTheInvoiceUnreconciledEvenWhenNetPlusVATAddsUp(t *testing.T) {
	s := newSequence(t, discountAsVAT, `{"total_label":"TOTAL :","total":"1550.24"}`)

	inv := s.extract(t)

	assert.False(t, inv.Reconciled(), "net 1550.24 + VAT 17.54 = total 1567.78 adds up, but the second read says 1550.24")
	require.NotEmpty(t, inv.Warnings())
	assert.Contains(t, inv.Warnings()[0], "1567.78")
	assert.Contains(t, inv.Warnings()[0], "1550.24")
}

func TestExtract_ASecondReadThatAgreesChangesNothing(t *testing.T) {
	s := newSequence(t, replyWithAmounts("100.00", "25.00", "125.00"), `{"total_label":"TOTAL","total":"125.00"}`)

	inv := s.extract(t)

	assert.True(t, inv.Reconciled())
	assert.Empty(t, inv.Warnings())
}

func TestExtract_ASecondReadThatCannotBeParsedLeavesTheTotalUnconfirmed(t *testing.T) {
	for name, reply := range map[string]string{
		"not json":     `I cannot read it`,
		"no total":     `{"total_label":"TOTAL"}`,
		"null total":   `{"total":null}`,
		"text total":   `{"total":"about 125"}`,
		"empty object": `{}`,
	} {
		t.Run(name, func(t *testing.T) {
			s := newSequence(t, replyWithAmounts("100.00", "25.00", "125.00"), reply)

			inv := s.extract(t)

			assert.False(t, inv.Reconciled())
			require.NotEmpty(t, inv.Warnings())
			assert.Contains(t, inv.Warnings()[0], "second read")
		})
	}
}

func TestExtract_TheSecondReadAsksOnlyForTheTotalAndSendsTheImageAgain(t *testing.T) {
	s := newSequence(t, replyWithAmounts("100.00", "25.00", "125.00"), `{"total":"125.00"}`)

	s.extract(t)

	require.Len(t, s.requests, 2)
	second := s.requests[1]["messages"].([]any)[0].(map[string]any)
	assert.NotEmpty(t, second["images"], "the second read must look at the invoice again, not at the first answer")
	text := second["content"].(string)
	assert.Contains(t, text, "total")
	assert.NotContains(t, text, "125.00", "the first answer must not be shown to the model, or it will just agree")
	assert.NotContains(t, text, "vat_amount")
}

func TestExtract_ACreditNoteTotalIsComparedWithItsSign(t *testing.T) {
	s := newSequence(t, replyWithAmounts("-100.00", "-25.00", "-125.00"), `{"total":"-125.00"}`)

	inv := s.extract(t)

	assert.True(t, inv.Reconciled())
}

func (s *sequence) extractText(t *testing.T, text string) extract.Invoice {
	t.Helper()
	c := &extract.Client{BaseURL: s.srv.URL, Model: "vision", TextModel: "text"}
	inv, err := c.ExtractText(context.Background(), text)
	require.NoError(t, err)
	return inv
}

func TestExtractText_ASecondReadOfTheTotalThatDisagreesLeavesTheInvoiceUnreconciled(t *testing.T) {
	s := newSequence(t, discountAsVAT, `{"total_label":"TOTAL :","total":"1550.24"}`)

	inv := s.extractText(t, "Discount 17.54\nTOTAL : 1550.24 USD")

	assert.False(t, inv.Reconciled())
	require.NotEmpty(t, inv.Warnings())
	assert.Contains(t, inv.Warnings()[0], "1567.78")
	assert.Contains(t, inv.Warnings()[0], "1550.24")
}

func TestExtractText_ASecondReadThatAgreesChangesNothing(t *testing.T) {
	s := newSequence(t, replyWithAmounts("100.00", "25.00", "125.00"), `{"total":"125.00"}`)

	inv := s.extractText(t, "Total 125.00")

	assert.True(t, inv.Reconciled())
	assert.Empty(t, inv.Warnings())
}

func TestExtractText_ASecondReadThatCannotBeParsedLeavesTheTotalUnconfirmed(t *testing.T) {
	s := newSequence(t, replyWithAmounts("100.00", "25.00", "125.00"), `I cannot read it`)

	inv := s.extractText(t, "Total 125.00")

	assert.False(t, inv.Reconciled())
	require.NotEmpty(t, inv.Warnings())
	assert.Contains(t, inv.Warnings()[0], "second read")
}

func TestExtractText_TheSecondReadGoesToTheTextModelWithTheFencedDocumentAndNoImage(t *testing.T) {
	s := newSequence(t, replyWithAmounts("100.00", "25.00", "125.00"), `{"total":"125.00"}`)

	s.extractText(t, "Total 125.00 SECRET-LINE")

	require.Len(t, s.requests, 2)
	second := s.requests[1]
	assert.Equal(t, "text", second["model"])
	msg := second["messages"].([]any)[0].(map[string]any)
	assert.NotContains(t, msg, "images")
	text := msg["content"].(string)
	assert.Contains(t, text, "SECRET-LINE", "the second read sees the document")
	assert.Contains(t, text, "untrusted", "and is told it is data")
	assert.Contains(t, text, "total")
	assert.NotContains(t, text, "vat_amount", "it asks only for the total")
	open := fenceOpen.FindStringSubmatch(text)
	require.NotNil(t, open, "the document is fenced")
	first := fenceOpen.FindStringSubmatch(s.requests[0]["messages"].([]any)[0].(map[string]any)["content"].(string))
	require.NotNil(t, first)
	assert.NotEqual(t, first[1], open[1], "a fresh fence token for the second request")
}
