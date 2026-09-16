package main

import "testing"

// nightlySchedule is how often the CronJob in mathias/infra runs this binary
// (k3s/apps/cobalt-dingo/overview-cronjob.yaml: "15 2 * * *").
const nightlySchedule = 24 * 60 * 60 // seconds

// The age backstop must outlast the schedule that drives it.
//
// Measured 2026-09-16: cacheMaxAge was 12h against a nightly run, so every
// scheduled run found the cache stale and re-read all 405 vouchers — one
// request each, 2 minutes, and growing with the books. The cache was paying
// its cost and returning nothing.
//
// The completeness check is unaffected by this and runs on EVERY invocation:
// one request for Fortnox's own count, compared against what is cached. That
// is the control. The age backstop covers only what a count cannot see — a
// voucher deleted AND replaced between runs, leaving the total unchanged —
// which is rare enough to bound in days rather than hours.
//
// This test exists because lowering the constant is a one-character change
// whose only symptom is a slower job that still produces correct output.
func TestCacheMaxAgeOutlastsTheSchedule(t *testing.T) {
	if int(cacheMaxAge.Seconds()) <= nightlySchedule {
		t.Fatalf("cacheMaxAge is %s, which is not longer than the %ds schedule — "+
			"every scheduled run would find the cache stale and re-read every voucher",
			cacheMaxAge, nightlySchedule)
	}
}

// ...but not so long that a blind spot persists for months. A full re-read is
// the only thing that detects a deleted-and-replaced voucher, so it has to
// happen on some human timescale.
func TestCacheMaxAgeStillForcesAPeriodicFullReRead(t *testing.T) {
	const month = 30 * 24 * 60 * 60
	if int(cacheMaxAge.Seconds()) > month {
		t.Fatalf("cacheMaxAge is %s — a deleted-and-replaced voucher could go "+
			"undetected for longer than a month", cacheMaxAge)
	}
}
