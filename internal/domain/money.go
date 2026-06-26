// Package domain contains the core business model and port interfaces for cobalt-dingo.
// It has no dependencies on external systems, frameworks, or infrastructure.
package domain

import (
	"fmt"
	"strconv"
	"strings"
)

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

// String formats the amount as "EUR 2,450.00" with thousands grouping.
func (m Money) String() string {
	neg := m.MinorUnits < 0
	u := m.MinorUnits
	if neg {
		u = -u
	}
	major := u / 100
	minor := u % 100

	s := strconv.FormatInt(major, 10)
	var b strings.Builder
	n := len(s)
	for i, d := range s {
		if i > 0 && (n-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(d)
	}
	sign := ""
	if neg {
		sign = "-"
	}
	return fmt.Sprintf("%s %s%s.%02d", m.Currency, sign, b.String(), minor)
}
