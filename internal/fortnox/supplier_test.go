package fortnox

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSupplierPaymentDetails_DecodesQuotedSupplierNumber reproduces the bug
// that blocked /invoices: Fortnox returns Supplier.SupplierNumber as a quoted
// string on GET /3/suppliers/{n}, and the int-typed field failed to decode,
// erroring the whole envelope and the invoice-enrichment hop the page makes.
// With FlexInt the quoted form decodes and IBAN/BIC are returned cleanly.
func TestSupplierPaymentDetails_DecodesQuotedSupplierNumber(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{"quoted SupplierNumber (the /invoices regression)", `{"Supplier":{"SupplierNumber":"1001","IBAN":"SE3550000000054910000003","BIC":"ESSESESS"}}`},
		{"bare SupplierNumber", `{"Supplier":{"SupplierNumber":1001,"IBAN":"SE3550000000054910000003","BIC":"ESSESESS"}}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tt.body))
			}))
			defer srv.Close()

			c := NewClient(srv.URL, "test-token", true)
			iban, bic, err := c.SupplierPaymentDetails(1001)
			require.NoError(t, err, "quoted SupplierNumber must not break the supplier decode")
			assert.Equal(t, "SE3550000000054910000003", iban)
			assert.Equal(t, "ESSESESS", bic)
		})
	}
}
