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
	got, why := VoucherCacheState{RemoteTotal: 405}.Assess(now, maxAge)
	if got != CacheStale {
		t.Errorf("got %q, want stale — reason: %s", got, why)
	}
	if !strings.Contains(why, "never") {
		t.Errorf("the reason must say it was never synced: %q", why)
	}
}

func TestVoucherCacheState_matchingCountsWithinMaxAgeIsFresh(t *testing.T) {
	s := VoucherCacheState{CachedCount: 405, RemoteTotal: 405, SyncedAt: now.Add(-time.Hour)}
	if got, why := s.Assess(now, maxAge); got != CacheFresh {
		t.Errorf("got %q, want fresh — reason: %s", got, why)
	}
}

// The cheap integrity check: Fortnox reports @TotalResources in one request, so
// a cache that has fallen behind is DETECTABLE rather than assumed. This is the
// whole reason caching is defensible here.
func TestVoucherCacheState_remoteAheadIsStaleAndNamesBothCounts(t *testing.T) {
	s := VoucherCacheState{CachedCount: 405, RemoteTotal: 412, SyncedAt: now.Add(-time.Minute)}
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
	s := VoucherCacheState{CachedCount: 405, RemoteTotal: 400, SyncedAt: now.Add(-time.Minute)}
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
	s := VoucherCacheState{CachedCount: 405, RemoteTotal: RemoteTotalUnknown, SyncedAt: now.Add(-2 * time.Hour)}
	got, why := s.Assess(now, maxAge)
	if got != CacheUnverified {
		t.Errorf("got %q, want unverified", got)
	}
	if !strings.Contains(why, "2026-09-14") {
		t.Errorf("an unverified answer must carry its as-of date: %q", why)
	}
}

func TestVoucherCacheState_unknownRemoteWithNoCacheIsStale(t *testing.T) {
	s := VoucherCacheState{CachedCount: 0, RemoteTotal: RemoteTotalUnknown}
	if got, _ := s.Assess(now, maxAge); got != CacheStale {
		t.Errorf("got %q, want stale — there is nothing to serve", got)
	}
}

// The age backstop exists because immutability is an assumption about the
// vendor, not a guarantee we can enforce. `lastmodified` is a documented
// parameter on /3/vouchers, which means Fortnox contemplates vouchers changing.
func TestVoucherCacheState_matchingCountsButTooOldIsStale(t *testing.T) {
	s := VoucherCacheState{CachedCount: 405, RemoteTotal: 405, SyncedAt: now.Add(-48 * time.Hour)}
	got, why := s.Assess(now, maxAge)
	if got != CacheStale {
		t.Errorf("got %q, want stale", got)
	}
	if !strings.Contains(why, "older") {
		t.Errorf("the reason must say it aged out: %q", why)
	}
}
