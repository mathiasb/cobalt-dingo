package domain

import (
	"fmt"
	"time"
)

// CacheFreshness is what may be done with a cached set of vouchers.
//
// Three states rather than two, for the same reason op-safe.sh has three: a
// binary usable/unusable has to fold "I could not check" into one of them, and
// both foldings are wrong. Treating it as usable hides a vendor outage behind
// confident numbers; treating it as unusable makes every report depend on
// Fortnox being reachable.
type CacheFreshness string

const (
	// CacheFresh means the cache was verified complete against Fortnox's own
	// count and is recent enough.
	CacheFresh CacheFreshness = "fresh"

	// CacheStale means it must be refreshed before use.
	CacheStale CacheFreshness = "stale"

	// CacheUnverified means it may be served but its completeness could NOT be
	// checked. The caller must surface the as-of date; presenting it as a
	// current answer is the failure this state exists to prevent.
	CacheUnverified CacheFreshness = "unverified"
)

// VoucherFetchVersion identifies the logic that produced the cached rows.
//
// It exists because the completeness check counts vouchers and can say nothing
// about their CONTENT. On 2026-09-14 the voucher detail fetch was missing its
// financialyear parameter, so four of 405 vouchers were stored with rows
// belonging to other vouchers. The count matched — 405 == 405 — so the cache
// reported itself verified complete and would have served those rows forever.
// Nothing in a count, a timestamp or a remote total can notice that.
//
// So the producing code is part of the cache key in all but name. BUMP THIS
// whenever the meaning or completeness of a cached row changes: a different
// endpoint, different parameters, a changed conversion. Forgetting to bump it
// is silent, which is why it sits next to the states it invalidates rather
// than in the adapter.
//
//	1 — initial: rows from GET /3/vouchers/{series}/{number}, no financial year
//	2 — detail fetch carries financialyear (v0.53.0); version 1 rows may hold
//	    rows belonging to a voucher of the same series and number in another year
const VoucherFetchVersion = 2

// RemoteTotalUnknown marks a completeness check that could not be performed.
// Zero cannot mean this: zero is a legitimate remote total for an empty year.
const RemoteTotalUnknown = -1

// VoucherCacheState is everything needed to decide whether cached vouchers may
// be used.
//
// Why a cache is defensible here at all: a posted voucher is immutable. Swedish
// bookkeeping corrects by adding a further voucher, never by editing or
// deleting one (docs/irreversible-operations.md). So the risk is not stale
// CONTENT, it is an INCOMPLETE cache reporting itself as whole — which is the
// exact shape of every wrong answer this integration produced on 2026-09-14.
//
// That risk is cheap to eliminate: Fortnox returns @TotalResources in the
// MetaInformation of a single list request. One request makes incompleteness
// detectable rather than assumed, and that is the only reason this design is
// acceptable for financial data.
type VoucherCacheState struct {
	// CachedCount is how many vouchers the cache holds for the year.
	CachedCount int

	// RemoteTotal is Fortnox's own @TotalResources, or RemoteTotalUnknown.
	RemoteTotal int

	// SyncedAt is when the cache was last verified complete. Zero means never.
	SyncedAt time.Time

	// FetchVersion is the VoucherFetchVersion that wrote these rows. Zero
	// means the rows predate versioning, so their provenance is unknown.
	FetchVersion int
}

// Assess reports what may be done with the cache, and why.
//
// The reason is returned rather than logged because it is meant to reach the
// operator: a financial figure served from cache should be able to say how it
// knows it is complete.
func (s VoucherCacheState) Assess(now time.Time, maxAge time.Duration) (CacheFreshness, string) {
	if s.SyncedAt.IsZero() {
		return CacheStale, "never synced"
	}

	// Before anything about counts or age: were these rows produced by the
	// logic this binary uses? A mismatch means the rows may be wrong in ways
	// no count can reveal, so it is not an old sync to age out — it is data of
	// unknown meaning.
	if s.FetchVersion != VoucherFetchVersion {
		return CacheStale, fmt.Sprintf(
			"cached rows were written by fetch version %d, this build uses %d — the count check cannot verify row CONTENT, so these rows must be re-read",
			s.FetchVersion, VoucherFetchVersion)
	}

	if s.RemoteTotal == RemoteTotalUnknown {
		if s.CachedCount == 0 {
			return CacheStale, "completeness could not be checked and the cache is empty — there is nothing to serve"
		}
		return CacheUnverified, fmt.Sprintf(
			"completeness could not be checked against Fortnox; %d voucher(s) as of %s",
			s.CachedCount, s.SyncedAt.Format("2006-01-02 15:04"))
	}

	// Fewer vouchers remotely than cached contradicts immutability. That is not
	// an ordinary miss to resync past — an assumption is wrong, and resyncing
	// silently would erase the evidence.
	if s.RemoteTotal < s.CachedCount {
		return CacheStale, fmt.Sprintf(
			"Fortnox reports FEWER vouchers (%d) than the cache holds (%d) — a posted voucher is immutable, so this contradicts an assumption and should be investigated, not resynced away",
			s.RemoteTotal, s.CachedCount)
	}

	if s.RemoteTotal > s.CachedCount {
		return CacheStale, fmt.Sprintf("cache holds %d voucher(s), Fortnox reports %d",
			s.CachedCount, s.RemoteTotal)
	}

	// Counts agree. The age backstop remains, because immutability is an
	// assumption about the vendor rather than something we can enforce —
	// `lastmodified` is a documented parameter on /3/vouchers, which means
	// Fortnox contemplates a voucher changing.
	if age := now.Sub(s.SyncedAt); age > maxAge {
		return CacheStale, fmt.Sprintf("counts agree at %d but the sync is older than %s (%s)",
			s.CachedCount, maxAge, age.Round(time.Minute))
	}

	return CacheFresh, fmt.Sprintf("%d voucher(s), verified complete against Fortnox at %s",
		s.CachedCount, s.SyncedAt.Format("2006-01-02 15:04"))
}
