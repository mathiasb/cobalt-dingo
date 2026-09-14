package report_test

import (
	"strings"
	"testing"

	"github.com/mathiasb/cobalt-dingo/internal/domain"
	"github.com/mathiasb/cobalt-dingo/internal/report"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The names that must not escape by default.
const (
	supplierName = "Some Bank AB"
	customerName = "Some Customer Ltd"
)

func withNames() domain.FinancialOverview {
	ov := overview()
	ov.UnbookedSupplier = []domain.SupplierInvoice{{
		InvoiceNumber: 144, SupplierName: supplierName, SupplierReference: "817744257",
		Balance: sek(8350.90), DueDate: "2026-09-10",
	}}
	ov.UnbookedCustomer = []domain.CustomerInvoice{{
		InvoiceNumber: 31, CustomerName: customerName, Balance: sek(260938), DueDate: "2026-08-12",
	}}
	ov.UnbalancedVouchers = []domain.UnbalancedVoucher{{
		Series: "C", Number: 7, Description: "Kundbet " + customerName + " (29)",
		Difference: sek(100),
		Rows: []domain.VoucherRow{
			{Account: 1930, Debit: sek(100), Description: "betalning " + customerName},
		},
	}}
	return ov
}

// Fail-closed: NamesRedacted is the ZERO value, so a caller who forgets to
// think about it gets redaction. Operator discipline is not a control
// (brain: structural-opt-in-marker-fail-closed).
func TestRender_defaultRedactsEveryCounterpartyName(t *testing.T) {
	out := report.Render(withNames())

	assert.NotContains(t, out, supplierName)
	assert.NotContains(t, out, customerName)
}

// Redaction must not cost actionability. Everything needed to find the invoice
// in Fortnox survives: the document number, the amount, the due date.
func TestRender_redactedOutputIsStillActionable(t *testing.T) {
	out := report.Render(withNames())

	assert.Contains(t, out, "144")
	assert.Contains(t, out, "8,350.90")
	assert.Contains(t, out, "2026-09-10")
	assert.Contains(t, out, "31")
	assert.Contains(t, out, "260,938.00")
	assert.Contains(t, out, "2026-08-12")
	// And it says a name was withheld, rather than rendering a blank that
	// looks like missing data.
	assert.Contains(t, out, "[name withheld]")
}

// Voucher descriptions name counterparties too — "Kundbet <customer> (29)" is
// a real production description. The unbalanced-voucher diagnostic must redact
// them while keeping the accounts and amounts that make it a diagnostic.
func TestRender_redactsVoucherDescriptionsAndRowDescriptions(t *testing.T) {
	out := report.Render(withNames())

	assert.NotContains(t, out, customerName)
	assert.Contains(t, out, "C/7", "the voucher is still identified")
	assert.Contains(t, out, "1930", "the account is still shown")
}

func TestRenderWithNames_showsThemWhenExplicitlyAsked(t *testing.T) {
	out := report.RenderWithNames(withNames())

	assert.Contains(t, out, supplierName)
	assert.Contains(t, out, customerName)
	assert.Contains(t, out, "Kundbet")
}

// BAS account descriptions are a standard chart of accounts, not counterparty
// data, and redacting them would make the report unreadable for no gain.
func TestRender_doesNotRedactAccountDescriptions(t *testing.T) {
	out := report.Render(withNames())
	assert.Contains(t, out, "Företagskonto")
}

// A name must not survive in any line of default output. Asserting on the
// whole string would pass if the name appeared only in a section a future
// change stops rendering; this checks every line that mentions the invoice.
func TestRender_noLineOfDefaultOutputCarriesAName(t *testing.T) {
	for _, line := range strings.Split(report.Render(withNames()), "\n") {
		require.NotContains(t, strings.ToLower(line), "some bank", "line: %s", line)
		require.NotContains(t, strings.ToLower(line), "some customer", "line: %s", line)
	}
}
