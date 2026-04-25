package fortnox

import (
	"context"
	"fmt"
	"time"

	"github.com/mathiasb/cobalt-dingo/internal/domain"
	rawfortnox "github.com/mathiasb/cobalt-dingo/internal/fortnox"
)

// CostCenterLedgerAdapter implements domain.CostCenterLedger using the Fortnox REST API.
// It reuses GeneralLedgerAdapter for voucher queries.
type CostCenterLedgerAdapter struct {
	baseURL string
	tokens  domain.TokenStore
	gl      *GeneralLedgerAdapter
}

// NewCostCenterLedgerAdapter returns a CostCenterLedgerAdapter pointed at baseURL,
// backed by the given token store, and reusing gl for voucher lookups.
func NewCostCenterLedgerAdapter(baseURL string, tokens domain.TokenStore, gl *GeneralLedgerAdapter) *CostCenterLedgerAdapter {
	return &CostCenterLedgerAdapter{baseURL: baseURL, tokens: tokens, gl: gl}
}

// client loads the tenant's access token and returns a ready-to-use raw Fortnox client.
func (a *CostCenterLedgerAdapter) client(ctx context.Context, tenantID domain.TenantID) (*rawfortnox.Client, error) {
	tok, err := a.tokens.Load(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("load token: %w", err)
	}
	return rawfortnox.NewClient(a.baseURL, tok.AccessToken), nil
}

// CostCenters implements domain.CostCenterLedger.
func (a *CostCenterLedgerAdapter) CostCenters(ctx context.Context, tenantID domain.TenantID) ([]domain.CostCenter, error) {
	c, err := a.client(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("cost center ledger cost centers: %w", err)
	}
	rows, err := c.ListCostCenters()
	if err != nil {
		return nil, fmt.Errorf("cost center ledger cost centers: %w", err)
	}

	centers := make([]domain.CostCenter, len(rows))
	for i, r := range rows {
		centers[i] = domain.CostCenter{
			Code:        r.Code,
			Description: r.Description,
			Active:      r.Active,
		}
	}
	return centers, nil
}

// CostCenterTransactions implements domain.CostCenterLedger.
// It finds the financial year covering from/to, fetches all vouchers for that year,
// and returns only the rows whose CostCenter field matches code.
func (a *CostCenterLedgerAdapter) CostCenterTransactions(ctx context.Context, tenantID domain.TenantID, code string, from, to time.Time) ([]domain.VoucherRow, error) {
	years, err := a.gl.FinancialYears(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("cost center transactions: %w", err)
	}

	yearID, err := findFinancialYear(years, from)
	if err != nil {
		return nil, fmt.Errorf("cost center transactions: %w", err)
	}

	vouchers, err := a.gl.Vouchers(ctx, tenantID, yearID, from, to)
	if err != nil {
		return nil, fmt.Errorf("cost center transactions: %w", err)
	}

	var rows []domain.VoucherRow
	for _, v := range vouchers {
		for _, row := range v.Rows {
			if row.CostCenter == code {
				rows = append(rows, row)
			}
		}
	}
	return rows, nil
}

// findFinancialYear returns the ID of the financial year whose [From, To] range
// contains date. Returns an error if no year covers the date.
func findFinancialYear(years []domain.FinancialYear, date time.Time) (int, error) {
	for _, y := range years {
		if !date.Before(y.From) && !date.After(y.To) {
			return y.ID, nil
		}
	}
	return 0, fmt.Errorf("no financial year covers %s", date.Format("2006-01-02"))
}
