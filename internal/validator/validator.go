// Package validator enforces BAS 2024 accounting rules and Swedish VAT
// requirements before vouchers are presented to the user or sent to Fortnox.
package validator

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/mathiasb/coo-agent/internal/api"
)

// accountFetcher is the subset of api.Client used by the validator.
type accountFetcher interface {
	ListAccounts(ctx context.Context) ([]api.Account, error)
}

// Validator validates vouchers against the live Fortnox chart of accounts.
type Validator struct {
	client   accountFetcher
	accounts map[int]api.Account // keyed by account number, populated at first use
}

// New creates a new Validator. The account list is lazily loaded on first validation.
func New(client accountFetcher) *Validator {
	return &Validator{client: client}
}

// ValidateVoucher validates a voucher before it is created in Fortnox.
// Returns a ValidationError listing all rule violations.
func (v *Validator) ValidateVoucher(ctx context.Context, voucher api.Voucher) error {
	if err := v.ensureAccounts(ctx); err != nil {
		return fmt.Errorf("validator: load accounts: %w", err)
	}

	var errs []error

	// Description must be set
	if voucher.Description == "" {
		errs = append(errs, errors.New("verifikationstext saknas"))
	}

	// Date must be set and within allowed range
	if voucher.VoucherDate == "" {
		errs = append(errs, errors.New("datum saknas"))
	} else {
		d, err := time.Parse("2006-01-02", voucher.VoucherDate)
		if err != nil {
			errs = append(errs, fmt.Errorf("ogiltigt datumformat %q (förväntat YYYY-MM-DD)", voucher.VoucherDate))
		} else {
			now := time.Now()
			if d.After(now) {
				errs = append(errs, fmt.Errorf("datum %s är i framtiden", voucher.VoucherDate))
			}
			if d.Before(now.AddDate(-1, 0, 0)) {
				errs = append(errs, fmt.Errorf("datum %s är mer än 1 år bakåt", voucher.VoucherDate))
			}
		}
	}

	// Must have rows
	if len(voucher.Rows) == 0 {
		errs = append(errs, errors.New("verifikationen har inga rader"))
	}

	// Each account must exist
	for i, row := range voucher.Rows {
		if _, ok := v.accounts[row.Account]; !ok {
			errs = append(errs, fmt.Errorf("rad %d: konto %d finns inte i kontoplanen", i+1, row.Account))
		}
	}

	// Debet must equal kredit
	var totalDebit, totalCredit float64
	for _, row := range voucher.Rows {
		totalDebit += row.Debit
		totalCredit += row.Credit
	}
	if !almostEqual(totalDebit, totalCredit) {
		errs = append(errs, fmt.Errorf("verifikationen är inte balanserad: debet %.2f ≠ kredit %.2f", totalDebit, totalCredit))
	}

	if len(errs) > 0 {
		return &ValidationError{Errors: errs}
	}
	return nil
}

// ValidationError aggregates multiple rule violations.
type ValidationError struct {
	Errors []error
}

func (e *ValidationError) Error() string {
	msg := fmt.Sprintf("validering misslyckades (%d fel):", len(e.Errors))
	for i, err := range e.Errors {
		msg += fmt.Sprintf("\n  %d. %s", i+1, err.Error())
	}
	return msg
}

// ensureAccounts loads and caches the chart of accounts if not already done.
func (v *Validator) ensureAccounts(ctx context.Context) error {
	if v.accounts != nil {
		return nil
	}
	accounts, err := v.client.ListAccounts(ctx)
	if err != nil {
		return err
	}
	v.accounts = make(map[int]api.Account, len(accounts))
	for _, a := range accounts {
		v.accounts[a.Number] = a
	}
	slog.Info("validator: loaded chart of accounts", "count", len(v.accounts))
	return nil
}

// almostEqual compares two floats with a tolerance of 0.005 SEK.
func almostEqual(a, b float64) bool {
	diff := a - b
	if diff < 0 {
		diff = -diff
	}
	return diff < 0.005
}
