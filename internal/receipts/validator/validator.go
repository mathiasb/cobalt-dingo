// Package validator enforces BAS 2024 accounting rules and Swedish VAT
// requirements. It depends on the AccountSource interface, never on a
// concrete Fortnox client, so it is fully testable without network access.
package validator

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Account is a BAS account entry as returned by the account source.
type Account struct {
	Number      int
	Description string
}

// VoucherRow is a single debit/credit line in a voucher.
type VoucherRow struct {
	Account int
	Debit   float64
	Credit  float64
}

// Voucher is the accounting document to be validated before creation.
type Voucher struct {
	Description string
	VoucherDate string // YYYY-MM-DD
	Rows        []VoucherRow
}

// AccountSource is the narrow interface the validator requires.
// *api.Client satisfies this interface; tests use a stub.
type AccountSource interface {
	ListAccounts(ctx context.Context) ([]Account, error)
}

// Validator validates vouchers against the live chart of accounts.
// The account list is fetched from the source once and cached.
type Validator struct {
	source   AccountSource
	accounts map[int]Account
}

// New creates a Validator. The AccountSource is called lazily on first validation.
func New(source AccountSource) (*Validator, error) {
	if source == nil {
		return nil, errors.New("validator: AccountSource must not be nil")
	}
	return &Validator{source: source}, nil
}

// ValidateVoucher checks all rules and returns a *ValidationError listing
// every violation found. Returns nil if the voucher is valid.
// All errors are collected before returning – no fail-fast.
func (v *Validator) ValidateVoucher(ctx context.Context, voucher Voucher) error {
	if err := v.loadAccounts(ctx); err != nil {
		return fmt.Errorf("validator: load accounts: %w", err)
	}

	var errs []error

	if strings.TrimSpace(voucher.Description) == "" {
		errs = append(errs, errors.New("verifikationstext saknas"))
	}

	if voucher.VoucherDate == "" {
		errs = append(errs, errors.New("datum saknas"))
	} else if date, err := time.Parse("2006-01-02", voucher.VoucherDate); err != nil {
		errs = append(errs, fmt.Errorf("ogiltigt datumformat %q – använd YYYY-MM-DD", voucher.VoucherDate))
	} else {
		errs = append(errs, validateDate(date)...)
	}

	if len(voucher.Rows) == 0 {
		errs = append(errs, errors.New("verifikationen har inga rader"))
	} else {
		errs = append(errs, v.validateRows(voucher.Rows)...)
	}

	if len(errs) > 0 {
		return &ValidationError{Errors: errs}
	}
	return nil
}

func validateDate(date time.Time) []error {
	var errs []error

	// A voucher date is a calendar DAY, not an instant. Comparing instants made
	// "today" read as the future whenever the local calendar had crossed
	// midnight and UTC had not — roughly 00:00–02:00 in Swedish summer time.
	// Both sides are reduced to a date before comparison.
	day := func(t time.Time) time.Time {
		y, m, d := t.Date()
		return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	}
	date = day(date)
	now := day(time.Now())

	if date.After(now) {
		errs = append(errs, fmt.Errorf("datum %s är i framtiden", date.Format("2006-01-02")))
	}
	if date.Before(now.AddDate(-1, 0, 0).Add(-24*time.Hour + time.Second)) {
		errs = append(errs, fmt.Errorf("datum %s är mer än 1 år bakåt", date.Format("2006-01-02")))
	}
	return errs
}

func (v *Validator) validateRows(rows []VoucherRow) []error {
	var errs []error
	var totalDebit, totalCredit float64

	for i, row := range rows {
		if _, ok := v.accounts[row.Account]; !ok {
			errs = append(errs, fmt.Errorf("rad %d: konto %d finns inte i kontoplanen", i+1, row.Account))
		}
		totalDebit += row.Debit
		totalCredit += row.Credit
	}

	if !almostEqual(totalDebit, totalCredit) {
		errs = append(errs, fmt.Errorf(
			"verifikationen är inte balanserad: debet %.2f ≠ kredit %.2f",
			totalDebit, totalCredit,
		))
	}
	return errs
}

// loadAccounts fetches and caches the chart of accounts on first call.
func (v *Validator) loadAccounts(ctx context.Context) error {
	if v.accounts != nil {
		return nil
	}
	accounts, err := v.source.ListAccounts(ctx)
	if err != nil {
		return err
	}
	v.accounts = make(map[int]Account, len(accounts))
	for _, a := range accounts {
		v.accounts[a.Number] = a
	}
	return nil
}

// almostEqual returns true when a and b differ by less than half an öre (0.005 SEK).
func almostEqual(a, b float64) bool {
	diff := a - b
	if diff < 0 {
		diff = -diff
	}
	return diff < 0.005
}

// ValidationError collects all rule violations for a voucher.
type ValidationError struct {
	Errors []error
}

func (e *ValidationError) Error() string {
	msgs := make([]string, len(e.Errors))
	for i, err := range e.Errors {
		msgs[i] = fmt.Sprintf("  %d. %s", i+1, err.Error())
	}
	return fmt.Sprintf("validering misslyckades (%d fel):\n%s", len(e.Errors), strings.Join(msgs, "\n"))
}
