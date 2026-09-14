package clitoken

import (
	"fmt"
	"strings"
	"time"

	"github.com/mathiasb/cobalt-dingo/internal/domain"
)

// SelectFinancialYear returns the financial year covering a date.
//
// Exists so a command does not take a financial-year ID as a hand-supplied
// number. Fortnox's year IDs are opaque and not in date order — the ID for
// 2026 on this account is not 2026 and not derivable — so passing one means
// looking it up and typing it, which is a guess with a plausible wrong answer.
//
// Inclusive at both ends, and refuses rather than choosing when years overlap.
func SelectFinancialYear(years []domain.FinancialYear, on time.Time) (domain.FinancialYear, error) {
	if len(years) == 0 {
		return domain.FinancialYear{}, fmt.Errorf("this company has no financial years")
	}

	day := on.Truncate(24 * time.Hour)
	var matches []domain.FinancialYear
	for _, y := range years {
		if !day.Before(y.From) && !day.After(y.To) {
			matches = append(matches, y)
		}
	}

	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return domain.FinancialYear{}, fmt.Errorf(
			"no financial year covers %s; available: %s",
			day.Format("2006-01-02"), describe(years))
	default:
		return domain.FinancialYear{}, fmt.Errorf(
			"financial years overlap on %s (%s) — refusing to attribute vouchers to one arbitrarily",
			day.Format("2006-01-02"), describe(matches))
	}
}

func describe(years []domain.FinancialYear) string {
	parts := make([]string, len(years))
	for i, y := range years {
		parts[i] = fmt.Sprintf("id %d: %s..%s", y.ID, y.From.Format("2006-01-02"), y.To.Format("2006-01-02"))
	}
	return strings.Join(parts, "; ")
}
