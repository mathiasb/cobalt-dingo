package domain

import "testing"

func TestMoneyString_ThousandsGrouping(t *testing.T) {
	tests := []struct {
		name  string
		money Money
		want  string
	}{
		{"under thousand", Money{MinorUnits: 84000, Currency: "EUR"}, "EUR 840.00"},
		{"exactly thousand", Money{MinorUnits: 100000, Currency: "EUR"}, "EUR 1,000.00"},
		{"four digits", Money{MinorUnits: 245000, Currency: "EUR"}, "EUR 2,450.00"},
		{"five digits", Money{MinorUnits: 1250000, Currency: "SEK"}, "SEK 12,500.00"},
		{"usd four digits", Money{MinorUnits: 189000, Currency: "USD"}, "USD 1,890.00"},
		{"millions", Money{MinorUnits: 100000000, Currency: "EUR"}, "EUR 1,000,000.00"},
		{"sub-unit retained", Money{MinorUnits: 175075, Currency: "EUR"}, "EUR 1,750.75"},
		{"zero", Money{MinorUnits: 0, Currency: "EUR"}, "EUR 0.00"},
		{"negative grouped", Money{MinorUnits: -245000, Currency: "EUR"}, "EUR -2,450.00"},
		{"negative under thousand", Money{MinorUnits: -50, Currency: "EUR"}, "EUR -0.50"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.money.String(); got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}
