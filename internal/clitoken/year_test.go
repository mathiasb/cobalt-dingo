package clitoken_test

import (
	"testing"
	"time"

	"github.com/mathiasb/cobalt-dingo/internal/clitoken"
	"github.com/mathiasb/cobalt-dingo/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func day(y, m, d int) time.Time { return time.Date(y, time.Month(m), d, 0, 0, 0, 0, time.UTC) }

func years() []domain.FinancialYear {
	return []domain.FinancialYear{
		{ID: 1, From: day(2024, 1, 1), To: day(2024, 12, 31)},
		{ID: 2, From: day(2025, 1, 1), To: day(2025, 12, 31)},
		{ID: 3, From: day(2026, 1, 1), To: day(2026, 12, 31)},
	}
}

func TestSelectFinancialYear_picksTheYearContainingTheDate(t *testing.T) {
	got, err := clitoken.SelectFinancialYear(years(), day(2026, 9, 14))
	require.NoError(t, err)
	assert.Equal(t, 3, got.ID)
}

// Inclusive at both ends: a year's last day belongs to that year, and posting
// on 31 December is ordinary.
func TestSelectFinancialYear_boundariesBelongToTheirYear(t *testing.T) {
	first, err := clitoken.SelectFinancialYear(years(), day(2025, 1, 1))
	require.NoError(t, err)
	assert.Equal(t, 2, first.ID)

	last, err := clitoken.SelectFinancialYear(years(), day(2025, 12, 31))
	require.NoError(t, err)
	assert.Equal(t, 2, last.ID)
}

// No year covering the date must name what IS available. "year not found" sends
// the operator to the API to find out what to pass instead.
func TestSelectFinancialYear_noneCoveringTheDateListsWhatExists(t *testing.T) {
	_, err := clitoken.SelectFinancialYear(years(), day(2030, 5, 1))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "2024-01-01")
	assert.Contains(t, err.Error(), "2026-12-31")
}

// Overlapping years are a Fortnox configuration this code cannot interpret.
// Picking either would attribute vouchers to a year arbitrarily.
func TestSelectFinancialYear_overlappingYearsAreRefused(t *testing.T) {
	overlapping := append(years(), domain.FinancialYear{ID: 4, From: day(2026, 7, 1), To: day(2027, 6, 30)})

	_, err := clitoken.SelectFinancialYear(overlapping, day(2026, 9, 14))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "overlap")
}

func TestSelectFinancialYear_emptyListIsAnError(t *testing.T) {
	_, err := clitoken.SelectFinancialYear(nil, day(2026, 9, 14))
	require.Error(t, err)
}
