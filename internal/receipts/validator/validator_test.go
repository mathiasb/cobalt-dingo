package validator_test

import (
	"context"
	"testing"
	"time"

	"github.com/mathiasb/cobalt-dingo/internal/receipts/validator"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// knownAccounts is the minimal BAS chart used across all test cases.
var knownAccounts = []validator.Account{
	{Number: 1930, Description: "Bankkonto"},
	{Number: 2610, Description: "Utgående moms 25%"},
	{Number: 2640, Description: "Ingående moms"},
	{Number: 2893, Description: "Skuld till närstående"},
	{Number: 3001, Description: "Försäljning tjänster 25%"},
	{Number: 6212, Description: "Mobiltelefon"},
}

func today() string { return time.Now().Format("2006-01-02") }

func TestValidator_AcceptsBalancedVoucher(t *testing.T) {
	v := validatorWithAccounts(t, knownAccounts)

	voucher := validator.Voucher{
		Description: "Telenor mars 2025",
		VoucherDate: today(),
		Rows: []validator.VoucherRow{
			{Account: 6212, Debit: 360.00},
			{Account: 2640, Debit: 90.00},
			{Account: 2893, Credit: 450.00},
		},
	}
	assert.NoError(t, v.ValidateVoucher(context.Background(), voucher))
}

func TestValidator_RejectsUnbalancedVoucher(t *testing.T) {
	v := validatorWithAccounts(t, knownAccounts)

	voucher := validator.Voucher{
		Description: "Telenor mars 2025",
		VoucherDate: today(),
		Rows: []validator.VoucherRow{
			{Account: 6212, Debit: 360.00},
			{Account: 2893, Credit: 500.00}, // wrong: 360 ≠ 500
		},
	}
	err := v.ValidateVoucher(context.Background(), voucher)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "balanserad")
}

func TestValidator_RejectsUnknownAccount(t *testing.T) {
	v := validatorWithAccounts(t, knownAccounts)

	voucher := validator.Voucher{
		Description: "Test",
		VoucherDate: today(),
		Rows: []validator.VoucherRow{
			{Account: 9999, Debit: 100.00},
			{Account: 2893, Credit: 100.00},
		},
	}
	err := v.ValidateVoucher(context.Background(), voucher)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "9999")
}

func TestValidator_RejectsFutureDate(t *testing.T) {
	v := validatorWithAccounts(t, knownAccounts)

	future := time.Now().AddDate(0, 0, 1).Format("2006-01-02")
	voucher := validator.Voucher{
		Description: "Test",
		VoucherDate: future,
		Rows: []validator.VoucherRow{
			{Account: 6212, Debit: 100.00},
			{Account: 2893, Credit: 100.00},
		},
	}
	err := v.ValidateVoucher(context.Background(), voucher)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "framtiden")
}

func TestValidator_RejectsDateOlderThanOneYear(t *testing.T) {
	v := validatorWithAccounts(t, knownAccounts)

	old := time.Now().AddDate(-1, 0, -1).Format("2006-01-02")
	voucher := validator.Voucher{
		Description: "Test",
		VoucherDate: old,
		Rows: []validator.VoucherRow{
			{Account: 6212, Debit: 100.00},
			{Account: 2893, Credit: 100.00},
		},
	}
	err := v.ValidateVoucher(context.Background(), voucher)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "år bakåt")
}

func TestValidator_RejectsMissingDescription(t *testing.T) {
	v := validatorWithAccounts(t, knownAccounts)

	voucher := validator.Voucher{
		Description: "",
		VoucherDate: today(),
		Rows: []validator.VoucherRow{
			{Account: 6212, Debit: 100.00},
			{Account: 2893, Credit: 100.00},
		},
	}
	err := v.ValidateVoucher(context.Background(), voucher)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "verifikationstext")
}

func TestValidator_RejectsMissingDate(t *testing.T) {
	v := validatorWithAccounts(t, knownAccounts)

	voucher := validator.Voucher{
		Description: "Test",
		VoucherDate: "",
		Rows: []validator.VoucherRow{
			{Account: 6212, Debit: 100.00},
			{Account: 2893, Credit: 100.00},
		},
	}
	err := v.ValidateVoucher(context.Background(), voucher)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "datum")
}

func TestValidator_RejectsEmptyRows(t *testing.T) {
	v := validatorWithAccounts(t, knownAccounts)

	voucher := validator.Voucher{
		Description: "Test",
		VoucherDate: today(),
		Rows:        []validator.VoucherRow{},
	}
	err := v.ValidateVoucher(context.Background(), voucher)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "rader")
}

func TestValidator_ReportsAllErrorsTogether(t *testing.T) {
	v := validatorWithAccounts(t, knownAccounts)

	// Multiple violations: no description, no date, unknown account, unbalanced.
	voucher := validator.Voucher{
		Description: "",
		VoucherDate: "",
		Rows: []validator.VoucherRow{
			{Account: 9999, Debit: 100.00},
			{Account: 2893, Credit: 200.00},
		},
	}
	err := v.ValidateVoucher(context.Background(), voucher)
	require.Error(t, err)

	var ve *validator.ValidationError
	require.ErrorAs(t, err, &ve)
	assert.GreaterOrEqual(t, len(ve.Errors), 3,
		"all validation errors must be collected and returned together, not fail-fast")
}

// TestValidator_ToleratesFloatingPointRounding ensures that amounts like
// 0.1 + 0.2 ≠ 0.3 (IEEE 754) don't cause false balance failures.
func TestValidator_ToleratesFloatingPointRounding(t *testing.T) {
	v := validatorWithAccounts(t, knownAccounts)

	voucher := validator.Voucher{
		Description: "Rounding test",
		VoucherDate: today(),
		Rows: []validator.VoucherRow{
			{Account: 6212, Debit: 0.1},
			{Account: 6212, Debit: 0.2},
			{Account: 2893, Credit: 0.3},
		},
	}
	assert.NoError(t, v.ValidateVoucher(context.Background(), voucher),
		"0.1+0.2 should be considered equal to 0.3 within floating-point tolerance")
}

func TestValidator_AcceptsDateOnBoundary(t *testing.T) {
	v := validatorWithAccounts(t, knownAccounts)

	// Exactly one year ago today should still be valid (boundary inclusive).
	boundary := time.Now().AddDate(-1, 0, 0).Format("2006-01-02")
	voucher := validator.Voucher{
		Description: "Gränsvärdestest",
		VoucherDate: boundary,
		Rows: []validator.VoucherRow{
			{Account: 6212, Debit: 100.00},
			{Account: 2893, Credit: 100.00},
		},
	}
	assert.NoError(t, v.ValidateVoucher(context.Background(), voucher))
}

// --- helpers ---

// stubAccountSource implements the AccountSource interface for testing.
type stubAccountSource struct {
	accounts []validator.Account
}

func (s *stubAccountSource) ListAccounts(_ context.Context) ([]validator.Account, error) {
	return s.accounts, nil
}

func validatorWithAccounts(t *testing.T, accounts []validator.Account) *validator.Validator {
	t.Helper()
	source := &stubAccountSource{accounts: accounts}
	v, err := validator.New(source)
	require.NoError(t, err)
	return v
}
