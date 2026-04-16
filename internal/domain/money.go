// Package domain contains the core business model and port interfaces for cobalt-dingo.
// It has no dependencies on external systems, frameworks, or infrastructure.
package domain

import "fmt"

// Money is an exact fixed-point monetary amount.
// MinorUnits stores the value as integer minor currency units (cents, öre, pence)
// to avoid floating-point arithmetic errors in financial calculations.
// EUR 2450.00 → MinorUnits: 245000.
type Money struct {
	MinorUnits int64
	Currency   string // ISO 4217
}

// MoneyFromFloat converts a float64 amount (as returned by the Fortnox API) to Money.
// Rounds to nearest minor unit.
func MoneyFromFloat(amount float64, currency string) Money {
	return Money{
		MinorUnits: int64(amount*100 + 0.5),
		Currency:   currency,
	}
}

// Float returns the amount as a float64, suitable for display and XML generation.
func (m Money) Float() float64 {
	return float64(m.MinorUnits) / 100
}

// String formats the amount as "EUR 2,450.00".
func (m Money) String() string {
	major := m.MinorUnits / 100
	minor := m.MinorUnits % 100
	if minor < 0 {
		minor = -minor
	}
	return fmt.Sprintf("%s %d.%02d", m.Currency, major, minor)
}
