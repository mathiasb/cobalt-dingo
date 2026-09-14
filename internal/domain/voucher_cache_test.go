package domain

import (
	"strings"
	"testing"
	"time"
)

var now = time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)

const maxAge = 24 * time.Hour

// Never synced is stale, not fresh-and-empty. An empty cache that reports
// itself usable would answer "you have no vouchers" for a company with 405.
func TestVoucherCacheState_neverSyncedIsStale(t *testing.T) {
	got, why := VoucherCacheState{RemoteTotal: 405, FetchVersion: VoucherFetchVersion}.Assess(now, maxAge)
	if got != CacheStale {
		t.Errorf("got %q, want stale — reason: %s", got, why)
	}
	if !strings.Contains(why, "never") {
		t.Errorf("the reason must say it was never synced: %q", why)
	}
}

func TestVoucherCacheState_matchingCountsWithinMaxAgeIsFresh(t *testing.T) {
	s := VoucherCacheState{CachedCount: 405, RemoteTotal: 405, SyncedAt: now.Add(-time.Hour), FetchVersion: VoucherFetchVersion}
	if got, why := s.Assess(now, maxAge); got != CacheFresh {
		t.Errorf("got %q, want fresh — reason: %s", got, why)
	}
}

// The cheap integrity check: Fortnox reports @TotalResources in one request, so
// a cache that has fallen behind is DETECTABLE rather than assumed. This is the
// whole reason caching is defensible here.
func TestVoucherCacheState_remoteAheadIsStaleAndNamesBothCounts(t *testing.T) {
	s := VoucherCacheState{CachedCount: 405, RemoteTotal: 412, SyncedAt: now.Add(-time.Minute), FetchVersion: VoucherFetchVersion}
	got, why := s.Assess(now, maxAge)
	if got != CacheStale {
		t.Errorf("got %q, want stale", got)
	}
	if !strings.Contains(why, "405") || !strings.Contains(why, "412") {
		t.Errorf("the reason must name both counts so the gap is visible: %q", why)
	}
}

// Fewer vouchers remotely than cached CONTRADICTS the immutability that makes
// this cache safe — a posted voucher is corrected by adding another, never
// removed. That is not an ordinary miss to resync past; it means an assumption
// is wrong and should be said out loud.
func TestVoucherCacheState_remoteBehindIsFlaggedAsContradiction(t *testing.T) {
	s := VoucherCacheState{CachedCount: 405, RemoteTotal: 400, SyncedAt: now.Add(-time.Minute), FetchVersion: VoucherFetchVersion}
	got, why := s.Assess(now, maxAge)
	if got != CacheStale {
		t.Errorf("got %q, want stale", got)
	}
	for _, want := range []string{"fewer", "immutab"} {
		if !strings.Contains(strings.ToLower(why), want) {
			t.Errorf("the reason must flag the contradiction (%q missing): %q", want, why)
		}
	}
}

// Unreachable Fortnox is its OWN state. Serving the cache silently would hide
// an outage behind confident numbers; refusing outright would make every report
// depend on the vendor being up. The caller decides, and must label it.
func TestVoucherCacheState_unknownRemoteTotalIsUnverifiedNotFresh(t *testing.T) {
	s := VoucherCacheState{CachedCount: 405, RemoteTotal: RemoteTotalUnknown, SyncedAt: now.Add(-2 * time.Hour), FetchVersion: VoucherFetchVersion}
	got, why := s.Assess(now, maxAge)
	if got != CacheUnverified {
		t.Errorf("got %q, want unverified", got)
	}
	if !strings.Contains(why, "2026-09-14") {
		t.Errorf("an unverified answer must carry its as-of date: %q", why)
	}
}

func TestVoucherCacheState_unknownRemoteWithNoCacheIsStale(t *testing.T) {
	s := VoucherCacheState{CachedCount: 0, RemoteTotal: RemoteTotalUnknown, FetchVersion: VoucherFetchVersion}
	if got, _ := s.Assess(now, maxAge); got != CacheStale {
		t.Errorf("got %q, want stale — there is nothing to serve", got)
	}
}

// The age backstop exists because immutability is an assumption about the
// vendor, not a guarantee we can enforce. `lastmodified` is a documented
// parameter on /3/vouchers, which means Fortnox contemplates vouchers changing.
func TestVoucherCacheState_matchingCountsButTooOldIsStale(t *testing.T) {
	s := VoucherCacheState{CachedCount: 405, RemoteTotal: 405, SyncedAt: now.Add(-48 * time.Hour), FetchVersion: VoucherFetchVersion}
	got, why := s.Assess(now, maxAge)
	if got != CacheStale {
		t.Errorf("got %q, want stale", got)
	}
	if !strings.Contains(why, "older") {
		t.Errorf("the reason must say it aged out: %q", why)
	}
}

// The count check proves how MANY vouchers are cached, never what is IN them.
// A change to how rows are read leaves the count identical and every cached
// row wrong — which is exactly what happened on 2026-09-14: the voucher detail
// fetch was missing its financialyear parameter, so four vouchers were cached
// with rows belonging to other vouchers. 405 == 405, so the cache reported
// itself verified complete and would have served the poisoned rows forever.
//
// So the version of the code that produced the rows is part of what
// "complete" means.
func TestVoucherCacheState_cacheWrittenByOtherFetchLogicIsStale(t *testing.T) {
	tests := []struct {
		name    string
		version int
	}{
		// Older: rows produced by logic since found wrong.
		{"older", VoucherFetchVersion - 1},
		// Newer: a rolled-back deployment reading rows a later version wrote.
		// Assuming forward compatibility would be a guess.
		{"newer", VoucherFetchVersion + 1},
		// Zero: written before versioning existed, so unknown provenance.
		{"unversioned", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := VoucherCacheState{
				CachedCount:  405,
				RemoteTotal:  405,
				SyncedAt:     now.Add(-time.Minute),
				FetchVersion: tt.version,
			}
			got, why := state.Assess(now, maxAge)
			if got != CacheStale {
				t.Errorf("got %q, want stale — reason: %s", got, why)
			}
			if !strings.Contains(why, "fetch") || !strings.Contains(why, "rows") {
				t.Errorf("reason must name the fetch version and the rows, got: %s", why)
			}
		})
	}
}

// The version check comes before the age backstop, so a mismatch is reported
// as what it is rather than as a merely old sync.
func TestVoucherCacheState_versionMismatchIsReportedAheadOfAge(t *testing.T) {
	state := VoucherCacheState{
		CachedCount:  405,
		RemoteTotal:  405,
		SyncedAt:     now.Add(-48 * time.Hour),
		FetchVersion: VoucherFetchVersion - 1,
	}
	_, why := state.Assess(now, maxAge)
	if strings.Contains(why, "older than") {
		t.Errorf("age reported instead of the version mismatch: %s", why)
	}
}

func TestVoucherCacheState_currentFetchVersionIsFresh(t *testing.T) {
	state := VoucherCacheState{
		CachedCount:  405,
		RemoteTotal:  405,
		SyncedAt:     now.Add(-time.Minute),
		FetchVersion: VoucherFetchVersion,
	}
	got, why := state.Assess(now, maxAge)
	if got != CacheFresh {
		t.Errorf("got %q, want fresh — reason: %s", got, why)
	}
}
