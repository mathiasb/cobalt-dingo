package fortnox

import (
	"context"
	"fmt"

	"github.com/mathiasb/cobalt-dingo/internal/domain"
	rawfortnox "github.com/mathiasb/cobalt-dingo/internal/fortnox"
)

// VoucherSourceAdapter reads vouchers from Fortnox for the voucher cache
// (ADR-0006).
//
// Separate from GeneralLedgerAdapter even though both wrap the same client:
// this one is the cache's source of truth, so it is hard-wired read-only and
// deliberately offers nothing but the two operations the cache policy needs.
// A shared adapter would mean the cache's filling path could grow a write.
type VoucherSourceAdapter struct {
	baseURL string
	tokens  domain.TokenStore
}

var _ domain.VoucherSource = (*VoucherSourceAdapter)(nil)

// NewVoucherSourceAdapter returns an adapter pointed at baseURL.
//
// There is no readOnly parameter. Filling a cache is a read, in every mode,
// and a caller cannot pass the wrong thing for a parameter that does not
// exist — which matters more here than elsewhere because this path runs
// unattended against live books.
func NewVoucherSourceAdapter(baseURL string, tokens domain.TokenStore) *VoucherSourceAdapter {
	return &VoucherSourceAdapter{baseURL: baseURL, tokens: tokens}
}

func (a *VoucherSourceAdapter) client(ctx context.Context, tenantID domain.TenantID) (*rawfortnox.Client, error) {
	tok, err := a.tokens.Load(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("load token: %w", err)
	}
	return rawfortnox.NewClient(a.baseURL, tok.AccessToken, true), nil
}

// RemoteTotal implements domain.VoucherSource. One request, reading Fortnox's
// @TotalResources — the cheap check the whole cache design rests on.
func (a *VoucherSourceAdapter) RemoteTotal(ctx context.Context, tenantID domain.TenantID, yearID int) (int, error) {
	c, err := a.client(ctx, tenantID)
	if err != nil {
		return 0, fmt.Errorf("voucher remote total: %w", err)
	}
	total, err := c.VoucherTotal(yearID)
	if err != nil {
		return 0, fmt.Errorf("voucher remote total: %w", err)
	}
	return total, nil
}

// FetchYear implements domain.VoucherSource.
//
// Rows are non-negotiable: the list endpoint returns vouchers with empty
// VoucherRows, so caching its output would store a complete-looking year in
// which every account, project and cost-centre figure is zero (#89). The cost
// is one request per voucher — roughly two minutes for a 405-voucher year
// under the rate limit — which is precisely why the result is worth caching.
//
// No date filtering. The cache is keyed by financial year because the year is
// the unit Fortnox reports a total for; a filtered subset stored under that
// key could not be checked against the remote count, and could match it by
// coincidence.
func (a *VoucherSourceAdapter) FetchYear(ctx context.Context, tenantID domain.TenantID, yearID int) ([]domain.Voucher, error) {
	c, err := a.client(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("voucher fetch year %d: %w", yearID, err)
	}
	raw, err := c.ListVouchersWithRows(yearID)
	if err != nil {
		return nil, fmt.Errorf("voucher fetch year %d: %w", yearID, err)
	}
	out := make([]domain.Voucher, 0, len(raw))
	for _, rv := range raw {
		out = append(out, convertVoucher(rv))
	}
	return out, nil
}
