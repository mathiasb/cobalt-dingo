package domain_test

import (
	"testing"

	"github.com/mathiasb/cobalt-dingo/internal/domain"
	"github.com/stretchr/testify/assert"
)

// Fortnox returns amounts as JSON floats, so every figure in the system passes
// through MoneyFromFloat. Negative amounts are not an edge case here: every
// credit, every liability and every equity balance is negative.
func TestMoneyFromFloat_roundsSymmetricallyAboutZero(t *testing.T) {
	tests := []struct {
		name   string
		amount float64
		want   int64
	}{
		{"positive exact", 435527.94, 43552794},
		{"negative exact", -410527.94, -41052794},
		{"negative small", -25000.00, -2500000},
		{"zero", 0, 0},
		{"positive half up", 0.005, 1},
		{"negative half away from zero", -0.005, -1},
		{"positive third", 0.334, 33},
		{"negative third", -0.334, -33},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, domain.MoneyFromFloat(tt.amount, "SEK").MinorUnits)
		})
	}
}

// The consequence that makes this worth a test rather than a comment: a
// balanced pair of entries must still balance after conversion. With
// truncation-toward-zero it did not, and the error accumulated once per
// negative row — which is once per voucher at minimum.
func TestMoneyFromFloat_aBalancedPairStaysBalanced(t *testing.T) {
	for _, amount := range []float64{100000, 75.50, 410527.94, 0.01, 1234.56, 19.99} {
		debit := domain.MoneyFromFloat(amount, "SEK")
		credit := domain.MoneyFromFloat(-amount, "SEK")
		assert.Zero(t, debit.MinorUnits+credit.MinorUnits, "%v did not net to zero", amount)
	}
}
