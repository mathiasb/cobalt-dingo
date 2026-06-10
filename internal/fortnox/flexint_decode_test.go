package fortnox

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFortnoxStructs_DecodeQuotedAndBareNumbers guards the FlexInt sweep:
// every Fortnox response struct that carries a quotable numeric identifier
// must decode whether Fortnox sends the value as a bare number or a quoted
// string. The bare-number form was already accepted; the quoted-string form
// is the shape that blocked /invoices (and travels across other endpoints).
func TestFortnoxStructs_DecodeQuotedAndBareNumbers(t *testing.T) {
	t.Run("SupplierRow.SupplierNumber", func(t *testing.T) {
		for _, form := range []string{`{"SupplierNumber":42,"IBAN":"SE1","BIC":"X"}`, `{"SupplierNumber":"42","IBAN":"SE1","BIC":"X"}`} {
			var r SupplierRow
			require.NoError(t, json.Unmarshal([]byte(form), &r), "form: %s", form)
			assert.Equal(t, FlexInt(42), r.SupplierNumber)
			assert.Equal(t, "SE1", r.IBAN)
		}
	})

	t.Run("AssetRow.ID", func(t *testing.T) {
		for _, form := range []string{`{"Id":7,"Number":"A-7"}`, `{"Id":"7","Number":"A-7"}`} {
			var r AssetRow
			require.NoError(t, json.Unmarshal([]byte(form), &r), "form: %s", form)
			assert.Equal(t, FlexInt(7), r.ID)
		}
	})

	t.Run("VoucherJSON.VoucherNumber+Year+rows.Account", func(t *testing.T) {
		bare := `{"VoucherSeries":"A","VoucherNumber":15,"Year":3,"VoucherRows":[{"Account":1930,"Debit":1.0}]}`
		quoted := `{"VoucherSeries":"A","VoucherNumber":"15","Year":"3","VoucherRows":[{"Account":"1930","Debit":1.0}]}`
		for _, form := range []string{bare, quoted} {
			var v VoucherJSON
			require.NoError(t, json.Unmarshal([]byte(form), &v), "form: %s", form)
			assert.Equal(t, FlexInt(15), v.VoucherNumber)
			assert.Equal(t, FlexInt(3), v.Year)
			require.Len(t, v.VoucherRows, 1)
			assert.Equal(t, FlexInt(1930), v.VoucherRows[0].Account)
		}
	})

	t.Run("SupplierInvoicePaymentRow.Number+InvoiceNumber", func(t *testing.T) {
		bare := `{"Number":5,"InvoiceNumber":900,"AmountCurrency":1.0,"Currency":"SEK"}`
		quoted := `{"Number":"5","InvoiceNumber":"900","AmountCurrency":1.0,"Currency":"SEK"}`
		for _, form := range []string{bare, quoted} {
			var r SupplierInvoicePaymentRow
			require.NoError(t, json.Unmarshal([]byte(form), &r), "form: %s", form)
			assert.Equal(t, FlexInt(5), r.Number)
			assert.Equal(t, FlexInt(900), r.InvoiceNumber)
		}
	})

	t.Run("CustomerInvoicePaymentRow.Number+InvoiceNumber", func(t *testing.T) {
		bare := `{"Number":6,"InvoiceNumber":901,"AmountCurrency":1.0,"Currency":"SEK"}`
		quoted := `{"Number":"6","InvoiceNumber":"901","AmountCurrency":1.0,"Currency":"SEK"}`
		for _, form := range []string{bare, quoted} {
			var r CustomerInvoicePaymentRow
			require.NoError(t, json.Unmarshal([]byte(form), &r), "form: %s", form)
			assert.Equal(t, FlexInt(6), r.Number)
			assert.Equal(t, FlexInt(901), r.InvoiceNumber)
		}
	})

	t.Run("AccountRow.Number+SRU", func(t *testing.T) {
		bare := `{"Number":1930,"SRU":7201,"Description":"Bank"}`
		quoted := `{"Number":"1930","SRU":"7201","Description":"Bank"}`
		for _, form := range []string{bare, quoted} {
			var r AccountRow
			require.NoError(t, json.Unmarshal([]byte(form), &r), "form: %s", form)
			assert.Equal(t, FlexInt(1930), r.Number)
			assert.Equal(t, FlexInt(7201), r.SRU)
		}
	})

	t.Run("FinancialYearRow.ID", func(t *testing.T) {
		for _, form := range []string{`{"Id":11,"FromDate":"2026-01-01","ToDate":"2026-12-31"}`, `{"Id":"11","FromDate":"2026-01-01","ToDate":"2026-12-31"}`} {
			var r FinancialYearRow
			require.NoError(t, json.Unmarshal([]byte(form), &r), "form: %s", form)
			assert.Equal(t, FlexInt(11), r.ID)
		}
	})
}
