package extract_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/mathiasb/cobalt-dingo/internal/extract"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func replyWithAmounts(net, vat, total string) string {
	n := "null"
	if net != "" {
		n = `"` + net + `"`
	}
	return fmt.Sprintf(`{"supplier":"A","invoice_number":"1","invoice_date":"2026-11-11","due_date":null,`+
		`"currency":"USD","net_total":%s,"vat_amount":"%s","total":"%s"}`, n, vat, total)
}

func extractAmounts(t *testing.T, net, vat, total string) extract.Invoice {
	t.Helper()
	srv := stub(t, replyWithAmounts(net, vat, total), nil)
	defer srv.Close()
	c := &extract.Client{BaseURL: srv.URL, Model: "m"}
	inv, err := c.Extract(context.Background(), []byte("x"))
	require.NoError(t, err)
	return inv
}

func TestReconciled_TrueWhenNetPlusVATEqualsTotal(t *testing.T) {
	inv := extractAmounts(t, "100.00", "25.00", "125.00")

	assert.True(t, inv.Reconciled())
	assert.Equal(t, int64(10000), inv.NetTotal)
	assert.Empty(t, inv.Warnings())
}

// Swedish invoices round to whole kronor (öresavrundning) as a separate line,
// so a gap of up to 50 öre is normal; beyond it is a misread.
func TestReconciled_AllowsRoundingUpToFiftyMinorUnitsButNotMore(t *testing.T) {
	tests := []struct {
		name  string
		total string
		want  bool
	}{
		{"exact", "125.00", true},
		{"fifty over", "125.50", true},
		{"fifty under", "124.50", true},
		{"fifty-one over", "125.51", false},
		{"fifty-one under", "124.49", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inv := extractAmounts(t, "100.00", "25.00", tt.total)

			assert.Equal(t, tt.want, inv.Reconciled())
		})
	}
}

// Three shapes a misread total can take, built so that net and VAT are read
// correctly and only the total is wrong. These are constructed cases: the
// "dropped digit" one was first seen as a LABEL error in the katanaml dataset
// (the model had read the printed total correctly), and the other two were seen
// as model misreads under an earlier prompt that no longer reproduces them.
func TestReconciled_FalseForTheThreeKnownWrongTotalShapes(t *testing.T) {
	tests := []struct {
		name, net, vat, wrongTotal string
	}{
		{"dropped digit", "1263943.50", "315985.87", "157929.37"}, // true total 1579929.37
		{"transposed digits", "7316.62", "1589.15", "8095.77"},    // true total 8905.77
		{"a different figure", "251.70", "62.92", "286.02"},       // true total 314.62
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inv := extractAmounts(t, tt.net, tt.vat, tt.wrongTotal)

			assert.False(t, inv.Reconciled(), "an unreconciled invoice is never a clean one")
			require.NotEmpty(t, inv.Warnings())
			assert.Contains(t, inv.Warnings()[0], "does not equal")
		})
	}
}

func TestReconciled_FalseWhenTheNetTotalWasNotRead(t *testing.T) {
	inv := extractAmounts(t, "", "25.00", "125.00")

	assert.False(t, inv.Reconciled(), "nothing to cross-check against")
	require.NotEmpty(t, inv.Warnings())
	assert.Contains(t, inv.Warnings()[0], "net total")
}

func TestWarnings_NameAnAmbiguousDateAlongsideReconciliation(t *testing.T) {
	srv := stub(t, `{"supplier":"A","invoice_number":"1","invoice_date":"02.11.2026","due_date":null,"currency":"SEK","net_total":"100.00","vat_amount":"25.00","total":"125.00"}`, nil)
	defer srv.Close()
	c := &extract.Client{BaseURL: srv.URL, Model: "m"}

	inv, err := c.Extract(context.Background(), []byte("x"))

	require.NoError(t, err)
	assert.True(t, inv.Reconciled())
	require.Len(t, inv.Warnings(), 1)
	assert.Contains(t, inv.Warnings()[0], "invoice date")
}

func TestExtract_AsksTheModelForTheNetTotalToo(t *testing.T) {
	var got map[string]any
	srv := stub(t, goodReply, &got)
	defer srv.Close()
	c := &extract.Client{BaseURL: srv.URL, Model: "m"}

	_, _ = c.Extract(context.Background(), []byte("x"))

	prompt := got["messages"].([]any)[0].(map[string]any)["content"].(string)
	assert.Contains(t, prompt, "net_total")
}

// Invoices built by ParseAmount cannot reach these values, but an Invoice is a
// plain struct, and a wrap here would make a garbled one look reconciled.
func TestReconciled_DoesNotWrapAroundForAnInvoiceBuiltByHand(t *testing.T) {
	const huge = int64(9223372036854775807)
	for _, total := range []int64{-2, 0, -3} {
		inv := extract.Invoice{NetKnown: true, NetTotal: huge, VATAmount: huge, Total: total}

		assert.False(t, inv.Reconciled(), "total %d", total)
		assert.NotEmpty(t, inv.Warnings(), "total %d", total)
	}
}
