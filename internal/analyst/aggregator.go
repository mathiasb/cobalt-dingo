// Package analyst provides shared utility functions for grouping, bucketing,
// aging, and summing financial data.
package analyst

import (
	"time"
)

const dateLayout = "2006-01-02"

// AgingReport classifies outstanding items into standard aging buckets.
type AgingReport struct {
	Current    int `json:"current"`
	Days1To30  int `json:"days_1_to_30"`
	Days31To60 int `json:"days_31_to_60"`
	Days61To90 int `json:"days_61_to_90"`
	Over90     int `json:"over_90"`
}

// AgingBuckets classifies due dates into aging buckets relative to today.
// Unparseable dates are silently skipped.
func AgingBuckets(dueDates []string, today time.Time) AgingReport {
	today = today.Truncate(24 * time.Hour)
	var r AgingReport
	for _, s := range dueDates {
		d, err := time.Parse(dateLayout, s)
		if err != nil {
			continue
		}
		days := int(today.Sub(d).Hours() / 24)
		switch {
		case days <= 0:
			r.Current++
		case days <= 30:
			r.Days1To30++
		case days <= 60:
			r.Days31To60++
		case days <= 90:
			r.Days61To90++
		default:
			r.Over90++
		}
	}
	return r
}

// GroupBy groups items by the key returned by keyFn.
func GroupBy[T any, K comparable](items []T, keyFn func(T) K) map[K][]T {
	result := make(map[K][]T)
	for _, item := range items {
		k := keyFn(item)
		result[k] = append(result[k], item)
	}
	return result
}

// SumMinorUnits sums a slice of int64 values (e.g. amounts in minor currency units).
func SumMinorUnits(values []int64) int64 {
	var total int64
	for _, v := range values {
		total += v
	}
	return total
}

// DaysOverdue returns the number of days past the due date, or 0 if not yet due.
// The dueDate must be in "2006-01-02" format; returns 0 on parse error.
func DaysOverdue(dueDate string, today time.Time) int {
	d, err := time.Parse(dateLayout, dueDate)
	if err != nil {
		return 0
	}
	today = today.Truncate(24 * time.Hour)
	days := int(today.Sub(d).Hours() / 24)
	if days < 0 {
		return 0
	}
	return days
}

// OrderedKeys returns the unique keys produced by keyFn in the order they first
// appeared in items.
func OrderedKeys[T any, K comparable](items []T, keyFn func(T) K) []K {
	seen := make(map[K]struct{})
	var keys []K
	for _, item := range items {
		k := keyFn(item)
		if _, ok := seen[k]; !ok {
			seen[k] = struct{}{}
			keys = append(keys, k)
		}
	}
	return keys
}
