package payment

import (
	"strings"
	"testing"
	"time"

	"github.com/mathiasb/cobalt-dingo/internal/domain"
)

// TestPAIN_NoThousandsSeparatorLeak guards that Money.String()'s thousands
// grouping (added in #34) never leaks into the generated PAIN.001 payment file.
// The XML path uses Money.Float()/fmtAmt, not String(); amounts here are ≥1000
// so a grouped separator would show if it ever regressed.
func TestPAIN_NoThousandsSeparatorLeak(t *testing.T) {
	invoices := []domain.EnrichedInvoice{
		{
			SupplierInvoice: domain.SupplierInvoice{
				InvoiceNumber: 2001, SupplierNumber: 2001, SupplierName: "Müller GmbH",
				Amount: domain.Money{MinorUnits: 245000, Currency: "EUR"}, DueDate: "2026-05-03",
			},
			IBAN: "DE89370400440532013000", BIC: "COBADEFFXXX",
		},
		{
			SupplierInvoice: domain.SupplierInvoice{
				InvoiceNumber: 2002, SupplierNumber: 2002, SupplierName: "Nordic Supply AB",
				Amount: domain.Money{MinorUnits: 1250000, Currency: "EUR"}, DueDate: "2026-05-03",
			},
			IBAN: "DE89370400440532013000", BIC: "COBADEFFXXX",
		},
	}
	debtor := Debtor{Name: "Cobalt Dingo AB", IBAN: "SE3550000000054910000003", BIC: "ESSESESS"}

	out, err := GeneratePAIN001(invoices, debtor, "MSG-FIXED-001", time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), ",") {
		t.Errorf("PAIN.001 contains a comma — thousands grouping leaked into the payment file:\n%s", out)
	}
	if !strings.Contains(string(out), "2450.00") || !strings.Contains(string(out), "12500.00") {
		t.Errorf("PAIN.001 missing expected ungrouped amounts (2450.00 / 12500.00):\n%s", out)
	}
}
