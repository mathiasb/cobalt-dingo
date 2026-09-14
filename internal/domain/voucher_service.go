package domain

import (
	"context"
	"fmt"
	"time"
)

// VoucherSource reads vouchers from the ERP.
//
// Split from VoucherCache so the service can be tested without a network, and
// because the two have very different costs: RemoteTotal is one request,
// FetchYear is one request per voucher.
type VoucherSource interface {
	// RemoteTotal is the ERP's own count for the year — Fortnox's
	// @TotalResources. One request, and the basis of every completeness check.
	RemoteTotal(ctx context.Context, tenantID TenantID, yearID int) (int, error)

	// FetchYear reads every voucher with its rows. Expensive: one request per
	// voucher, roughly two minutes for a 405-voucher year under the Fortnox
	// rate limit.
	FetchYear(ctx context.Context, tenantID TenantID, yearID int) ([]Voucher, error)
}

// VoucherSet is vouchers together with how much they can be trusted.
//
// Freshness, AsOf and Reason travel WITH the data rather than being returned
// separately, so a caller cannot present a figure without them by accident.
// ADR-0006's kill condition is a report that shows a cached number with no
// as-of date; making the three inseparable is the structural answer to it.
type VoucherSet struct {
	Vouchers  []Voucher
	Freshness CacheFreshness
	AsOf      time.Time
	Reason    string
}

// VoucherService serves vouchers from cache when completeness can be proved,
// and refreshes when it cannot (ADR-0006).
type VoucherService struct {
	cache  VoucherCache
	src    VoucherSource
	maxAge time.Duration
	now    func() time.Time
}

// NewVoucherService constructs the service. now is injected so the age
// backstop is testable without sleeping.
func NewVoucherService(cache VoucherCache, src VoucherSource, maxAge time.Duration, now func() time.Time) *VoucherService {
	if now == nil {
		now = time.Now
	}
	return &VoucherService{cache: cache, src: src, maxAge: maxAge, now: now}
}

// Vouchers returns the year's vouchers, refreshing the cache if its
// completeness cannot be established.
func (s *VoucherService) Vouchers(ctx context.Context, tenantID TenantID, yearID int) (VoucherSet, error) {
	cached, syncedAt, err := s.cache.LoadYear(ctx, tenantID, yearID)
	if err != nil {
		return VoucherSet{}, fmt.Errorf("read voucher cache: %w", err)
	}

	remoteTotal := RemoteTotalUnknown
	if total, terr := s.src.RemoteTotal(ctx, tenantID, yearID); terr == nil {
		remoteTotal = total
	} else if len(cached) == 0 {
		// Nothing to serve and no way to check. An empty result here would
		// read as "this company has no vouchers", which is the confident-wrong
		// shape this design exists to avoid.
		return VoucherSet{}, fmt.Errorf("voucher count unavailable and nothing cached: %w", terr)
	}

	state := VoucherCacheState{CachedCount: len(cached), RemoteTotal: remoteTotal, SyncedAt: syncedAt}
	freshness, reason := state.Assess(s.now(), s.maxAge)

	// All three states handled explicitly. The exhaustive linter objected to
	// stale falling through, and it was right to: a new freshness state added
	// later would silently take the refresh path, which is the wrong default
	// for something whose whole job is to not guess.
	switch freshness {
	case CacheFresh:
		return VoucherSet{Vouchers: cached, Freshness: CacheFresh, AsOf: syncedAt, Reason: reason}, nil

	case CacheUnverified:
		// Serve it, labelled. The caller decides whether an unverified figure
		// is acceptable; hiding the distinction is what is not allowed.
		return VoucherSet{Vouchers: cached, Freshness: CacheUnverified, AsOf: syncedAt, Reason: reason}, nil

	case CacheStale:
		return s.refresh(ctx, tenantID, yearID, reason)
	}

	return VoucherSet{}, fmt.Errorf("unknown cache freshness %q — refusing to guess", freshness)
}

// refresh fetches the year and caches it.
//
// It deliberately does NOT fall back to the cache on failure. The only reason
// it is here is that completeness could not be established, so serving the old
// set would mean knowingly returning an incomplete answer — worse than an
// error, because the caller cannot tell.
func (s *VoucherService) refresh(ctx context.Context, tenantID TenantID, yearID int, why string) (VoucherSet, error) {
	fetched, err := s.src.FetchYear(ctx, tenantID, yearID)
	if err != nil {
		return VoucherSet{}, fmt.Errorf("refresh vouchers (%s): %w", why, err)
	}

	// Re-check the total AFTER the fetch. A 405-voucher year takes about two
	// minutes, which is long enough for a voucher to be posted while we read —
	// and a torn read is indistinguishable from a complete one by size alone.
	after, terr := s.src.RemoteTotal(ctx, tenantID, yearID)
	now := s.now()

	if terr != nil || after != len(fetched) {
		reason := fmt.Sprintf("read %d voucher(s) but the count could not be confirmed afterwards", len(fetched))
		if terr == nil {
			reason = fmt.Sprintf("read %d voucher(s) while Fortnox moved to %d — the books changed during the read", len(fetched), after)
		}
		// NOT cached: storing a torn read with a sync timestamp would make it
		// look verified on the next request, which is the one thing the sync
		// row exists to prevent.
		return VoucherSet{Vouchers: fetched, Freshness: CacheUnverified, AsOf: now, Reason: reason}, nil
	}

	if err := s.cache.SaveYear(ctx, tenantID, yearID, fetched, after, now); err != nil {
		// Surfaced rather than swallowed. A set that saved its vouchers but not
		// its sync row would be served as fresh next time without ever having
		// been verified.
		return VoucherSet{}, fmt.Errorf("cache refreshed vouchers: %w", err)
	}

	return VoucherSet{
		Vouchers:  fetched,
		Freshness: CacheFresh,
		AsOf:      now,
		Reason:    fmt.Sprintf("%d voucher(s), fetched and verified complete at %s", len(fetched), now.Format("2006-01-02 15:04")),
	}, nil
}
