package fortnox

import (
	"context"
	"sync"
	"time"

	"github.com/mathiasb/cobalt-dingo/internal/domain"
)

// cacheEntry holds a cached IBAN/BIC pair with its expiry timestamp.
type cacheEntry struct {
	iban      string
	bic       string
	expiresAt time.Time
}

// CachingEnricher wraps a domain.SupplierEnricher and caches results per
// (tenantID, supplierNumber) with a configurable TTL. It prevents redundant
// Fortnox API calls across requests — the raw connector's per-request dedup
// only covers a single request lifecycle.
//
// Safe for concurrent use. Cache eviction is lazy (on next read).
type CachingEnricher struct {
	inner domain.SupplierEnricher
	ttl   time.Duration
	mu    sync.Mutex
	cache map[cacheKey]cacheEntry
}

type cacheKey struct {
	tenantID       domain.TenantID
	supplierNumber int
}

// NewCachingEnricher wraps inner with a TTL cache. A TTL of 5 minutes is
// appropriate for production; use a shorter value in tests.
func NewCachingEnricher(inner domain.SupplierEnricher, ttl time.Duration) *CachingEnricher {
	return &CachingEnricher{
		inner: inner,
		ttl:   ttl,
		cache: make(map[cacheKey]cacheEntry),
	}
}

// SupplierPaymentDetails implements domain.SupplierEnricher.
// Returns cached values when present and not expired, otherwise delegates to
// the inner enricher and caches the result.
func (c *CachingEnricher) SupplierPaymentDetails(ctx context.Context, tenantID domain.TenantID, supplierNumber int) (string, string, error) {
	key := cacheKey{tenantID: tenantID, supplierNumber: supplierNumber}

	c.mu.Lock()
	if entry, ok := c.cache[key]; ok && time.Now().Before(entry.expiresAt) {
		c.mu.Unlock()
		return entry.iban, entry.bic, nil
	}
	c.mu.Unlock()

	iban, bic, err := c.inner.SupplierPaymentDetails(ctx, tenantID, supplierNumber)
	if err != nil {
		return "", "", err
	}

	c.mu.Lock()
	c.cache[key] = cacheEntry{iban: iban, bic: bic, expiresAt: time.Now().Add(c.ttl)}
	c.mu.Unlock()

	return iban, bic, nil
}

// Invalidate removes a single supplier from the cache (e.g. on WebSocket
// supplier-updated-v1 event).
func (c *CachingEnricher) Invalidate(tenantID domain.TenantID, supplierNumber int) {
	c.mu.Lock()
	delete(c.cache, cacheKey{tenantID: tenantID, supplierNumber: supplierNumber})
	c.mu.Unlock()
}
