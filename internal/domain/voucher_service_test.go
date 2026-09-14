package domain

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type fakeCache struct {
	vouchers []Voucher
	syncedAt time.Time
	version  int
	saved    []Voucher
	savedTot int
	saveErr  error
	loadErr  error
}

func (f *fakeCache) LoadYear(context.Context, TenantID, int) (CachedVouchers, error) {
	// Defaults to the current version: these tests exercise the count, age and
	// torn-read rules, and a zero version would make every one of them stale
	// for the wrong reason.
	v := f.version
	if v == 0 {
		v = VoucherFetchVersion
	}
	return CachedVouchers{Vouchers: f.vouchers, SyncedAt: f.syncedAt, FetchVersion: v}, f.loadErr
}

func (f *fakeCache) SaveYear(_ context.Context, _ TenantID, _ int, v []Voucher, remoteTotal int, _ time.Time) error {
	f.saved, f.savedTot = v, remoteTotal
	return f.saveErr
}

type fakeSource struct {
	totals   []int // consumed in order, so a mid-fetch change can be simulated
	totalErr error
	fetched  []Voucher
	fetchErr error
	fetches  int
	totalN   int
}

func (f *fakeSource) RemoteTotal(context.Context, TenantID, int) (int, error) {
	if f.totalErr != nil {
		return 0, f.totalErr
	}
	i := f.totalN
	if i >= len(f.totals) {
		i = len(f.totals) - 1
	}
	f.totalN++
	return f.totals[i], nil
}

func (f *fakeSource) FetchYear(context.Context, TenantID, int) ([]Voucher, error) {
	f.fetches++
	return f.fetched, f.fetchErr
}

func svc(c VoucherCache, s VoucherSource, at time.Time) *VoucherService {
	return NewVoucherService(c, s, 24*time.Hour, func() time.Time { return at })
}

func vouchersN(n int) []Voucher {
	out := make([]Voucher, n)
	for i := range out {
		out[i] = Voucher{Series: "A", Number: i + 1, TransactionDate: "2026-03-01"}
	}
	return out
}

var at = time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)

// A verified-complete cache is served without re-fetching. That is the whole
// point: the fetch costs one request per voucher, about two minutes for a year.
func TestVoucherService_freshCacheIsServedWithoutFetching(t *testing.T) {
	c := &fakeCache{vouchers: vouchersN(3), syncedAt: at.Add(-time.Hour)}
	s := &fakeSource{totals: []int{3}}

	got, err := svc(c, s, at).Vouchers(context.Background(), "t", 3)
	if err != nil {
		t.Fatalf("vouchers: %v", err)
	}
	if s.fetches != 0 {
		t.Errorf("a fresh cache must not trigger a fetch, got %d", s.fetches)
	}
	if got.Freshness != CacheFresh || len(got.Vouchers) != 3 {
		t.Errorf("got %q with %d vouchers", got.Freshness, len(got.Vouchers))
	}
	if got.AsOf.IsZero() || got.Reason == "" {
		t.Error("every result must carry an as-of time and a reason")
	}
}

func TestVoucherService_staleCacheIsRefreshedAndSaved(t *testing.T) {
	c := &fakeCache{vouchers: vouchersN(3), syncedAt: at.Add(-time.Hour)}
	s := &fakeSource{totals: []int{5}, fetched: vouchersN(5)}

	got, err := svc(c, s, at).Vouchers(context.Background(), "t", 3)
	if err != nil {
		t.Fatalf("vouchers: %v", err)
	}
	if s.fetches != 1 {
		t.Errorf("want exactly one fetch, got %d", s.fetches)
	}
	if len(c.saved) != 5 || c.savedTot != 5 {
		t.Errorf("the refreshed set must be saved with the total it was verified against: %d/%d", len(c.saved), c.savedTot)
	}
	if got.Freshness != CacheFresh || len(got.Vouchers) != 5 {
		t.Errorf("got %q with %d vouchers", got.Freshness, len(got.Vouchers))
	}
}

// The fetch takes minutes, so the books can move while it runs. Re-checking the
// total afterwards turns a torn read into a labelled one instead of a confident
// wrong number.
func TestVoucherService_countChangingDuringTheFetchIsUnverified(t *testing.T) {
	c := &fakeCache{}
	s := &fakeSource{totals: []int{5, 6}, fetched: vouchersN(5)}

	got, err := svc(c, s, at).Vouchers(context.Background(), "t", 3)
	if err != nil {
		t.Fatalf("vouchers: %v", err)
	}
	if got.Freshness != CacheUnverified {
		t.Errorf("got %q, want unverified", got.Freshness)
	}
	if !strings.Contains(got.Reason, "6") || !strings.Contains(got.Reason, "5") {
		t.Errorf("the reason must name both totals: %q", got.Reason)
	}
	if len(c.saved) != 0 {
		t.Error("a torn read must NOT be saved as a verified cache")
	}
}

// Fortnox unreachable with a populated cache: serve it, labelled. Refusing
// would make every report depend on the vendor being up.
func TestVoucherService_unreachableVendorServesTheCacheLabelled(t *testing.T) {
	c := &fakeCache{vouchers: vouchersN(3), syncedAt: at.Add(-2 * time.Hour)}
	s := &fakeSource{totalErr: errors.New("connection refused")}

	got, err := svc(c, s, at).Vouchers(context.Background(), "t", 3)
	if err != nil {
		t.Fatalf("vouchers: %v", err)
	}
	if got.Freshness != CacheUnverified || len(got.Vouchers) != 3 {
		t.Errorf("got %q with %d vouchers", got.Freshness, len(got.Vouchers))
	}
	if s.fetches != 0 {
		t.Error("a fetch cannot help when the vendor is unreachable")
	}
}

// Unreachable with nothing cached is an error. There is no honest answer to
// give, and an empty result would read as "no vouchers".
func TestVoucherService_unreachableVendorWithEmptyCacheIsAnError(t *testing.T) {
	c := &fakeCache{}
	s := &fakeSource{totalErr: errors.New("connection refused")}

	if _, err := svc(c, s, at).Vouchers(context.Background(), "t", 3); err == nil {
		t.Fatal("want an error: there is nothing to serve and nothing to check against")
	}
}

// A refresh that fails must not fall back to serving a cache already KNOWN to
// be incomplete — the counts disagreed, which is why the refresh was attempted.
func TestVoucherService_failedRefreshDoesNotServeAKnownIncompleteCache(t *testing.T) {
	c := &fakeCache{vouchers: vouchersN(3), syncedAt: at.Add(-time.Hour)}
	s := &fakeSource{totals: []int{5}, fetchErr: errors.New("429 after retries")}

	_, err := svc(c, s, at).Vouchers(context.Background(), "t", 3)
	if err == nil {
		t.Fatal("want an error rather than a knowingly incomplete answer")
	}
	if !strings.Contains(err.Error(), "429") {
		t.Errorf("the cause must survive: %v", err)
	}
}

// A cache that saves but cannot record its sync would be served as fresh next
// time while never having been verified. The save error must surface.
func TestVoucherService_saveFailureIsReported(t *testing.T) {
	c := &fakeCache{saveErr: errors.New("disk full")}
	s := &fakeSource{totals: []int{2}, fetched: vouchersN(2)}

	if _, err := svc(c, s, at).Vouchers(context.Background(), "t", 3); err == nil {
		t.Fatal("want the save failure reported, not swallowed")
	}
}
