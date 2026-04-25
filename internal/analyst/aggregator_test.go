package analyst_test

import (
	"testing"
	"time"

	"github.com/mathiasb/cobalt-dingo/internal/analyst"
)

func TestAgingBuckets(t *testing.T) {
	today := time.Date(2026, 4, 25, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name     string
		dueDates []string
		want     analyst.AgingReport
	}{
		{
			name:     "single current (due today)",
			dueDates: []string{"2026-04-25"},
			want:     analyst.AgingReport{Current: 1},
		},
		{
			name:     "single current (due in future)",
			dueDates: []string{"2026-05-01"},
			want:     analyst.AgingReport{Current: 1},
		},
		{
			name: "mixed buckets one in each",
			dueDates: []string{
				"2026-04-25", // current (0 days)
				"2026-04-10", // 15 days overdue → 1-30
				"2026-02-28", // 56 days overdue → 31-60
				"2026-01-20", // 95 days overdue → 61-90? no, 95 > 90
				"2025-12-01", // 145 days overdue → over90
			},
			want: analyst.AgingReport{
				Current:    1,
				Days1To30:  1,
				Days31To60: 1,
				Over90:     2,
			},
		},
		{
			name:     "empty input",
			dueDates: []string{},
			want:     analyst.AgingReport{},
		},
		{
			name:     "skip unparseable date",
			dueDates: []string{"not-a-date", "2026-04-25"},
			want:     analyst.AgingReport{Current: 1},
		},
		{
			name:     "61-90 bucket",
			dueDates: []string{"2026-02-10"}, // 74 days overdue
			want:     analyst.AgingReport{Days61To90: 1},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := analyst.AgingBuckets(tc.dueDates, today)
			if got != tc.want {
				t.Errorf("AgingBuckets(%v, %v) = %+v, want %+v", tc.dueDates, today, got, tc.want)
			}
		})
	}
}

func TestGroupByString(t *testing.T) {
	type item struct {
		name  string
		group string
	}
	items := []item{
		{"a", "foo"},
		{"b", "bar"},
		{"c", "foo"},
		{"d", "baz"},
		{"e", "bar"},
	}

	got := analyst.GroupBy(items, func(i item) string { return i.group })

	tests := []struct {
		key       string
		wantCount int
	}{
		{"foo", 2},
		{"bar", 2},
		{"baz", 1},
	}

	for _, tc := range tests {
		group, ok := got[tc.key]
		if !ok {
			t.Errorf("key %q not found in result", tc.key)
			continue
		}
		if len(group) != tc.wantCount {
			t.Errorf("GroupBy key %q: got %d items, want %d", tc.key, len(group), tc.wantCount)
		}
	}

	if len(got) != 3 {
		t.Errorf("GroupBy: got %d groups, want 3", len(got))
	}
}

func TestSumMinorUnits(t *testing.T) {
	values := []int64{100, 250, 75}
	got := analyst.SumMinorUnits(values)
	want := int64(425)
	if got != want {
		t.Errorf("SumMinorUnits(%v) = %d, want %d", values, got, want)
	}
}

func TestDaysOverdue(t *testing.T) {
	today := time.Date(2026, 4, 25, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name    string
		dueDate string
		want    int
	}{
		{
			name:    "future date returns 0",
			dueDate: "2026-05-10",
			want:    0,
		},
		{
			name:    "past date returns positive days",
			dueDate: "2026-04-15",
			want:    10,
		},
		{
			name:    "today returns 0",
			dueDate: "2026-04-25",
			want:    0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := analyst.DaysOverdue(tc.dueDate, today)
			if got != tc.want {
				t.Errorf("DaysOverdue(%q, %v) = %d, want %d", tc.dueDate, today, got, tc.want)
			}
		})
	}
}

func TestOrderedKeys(t *testing.T) {
	type item struct {
		val string
		key string
	}
	items := []item{
		{"1", "alpha"},
		{"2", "beta"},
		{"3", "alpha"},
		{"4", "gamma"},
		{"5", "beta"},
	}

	got := analyst.OrderedKeys(items, func(i item) string { return i.key })

	want := []string{"alpha", "beta", "gamma"}
	if len(got) != len(want) {
		t.Fatalf("OrderedKeys: got %d keys %v, want %d keys %v", len(got), got, len(want), want)
	}
	for i, k := range want {
		if got[i] != k {
			t.Errorf("OrderedKeys[%d] = %q, want %q", i, got[i], k)
		}
	}
}
