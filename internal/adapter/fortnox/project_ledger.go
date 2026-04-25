package fortnox

import (
	"context"
	"fmt"
	"time"

	"github.com/mathiasb/cobalt-dingo/internal/domain"
	rawfortnox "github.com/mathiasb/cobalt-dingo/internal/fortnox"
)

// ProjectLedgerAdapter implements domain.ProjectLedger using the Fortnox REST API.
// It delegates voucher queries to GeneralLedgerAdapter to reuse existing GL logic.
type ProjectLedgerAdapter struct {
	baseURL string
	tokens  domain.TokenStore
	gl      *GeneralLedgerAdapter
}

// NewProjectLedgerAdapter returns a ProjectLedgerAdapter pointed at baseURL,
// backed by the given token store, and using gl for voucher queries.
func NewProjectLedgerAdapter(baseURL string, tokens domain.TokenStore, gl *GeneralLedgerAdapter) *ProjectLedgerAdapter {
	return &ProjectLedgerAdapter{baseURL: baseURL, tokens: tokens, gl: gl}
}

// client loads the tenant's access token and returns a ready-to-use raw Fortnox client.
func (a *ProjectLedgerAdapter) client(ctx context.Context, tenantID domain.TenantID) (*rawfortnox.Client, error) {
	tok, err := a.tokens.Load(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("load token: %w", err)
	}
	return rawfortnox.NewClient(a.baseURL, tok.AccessToken), nil
}

// Projects implements domain.ProjectLedger.
func (a *ProjectLedgerAdapter) Projects(ctx context.Context, tenantID domain.TenantID) ([]domain.Project, error) {
	c, err := a.client(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("project ledger projects: %w", err)
	}
	rows, err := c.ListProjects()
	if err != nil {
		return nil, fmt.Errorf("project ledger projects: %w", err)
	}

	projects := make([]domain.Project, len(rows))
	for i, r := range rows {
		projects[i] = domain.Project{
			Number:      r.ProjectNumber,
			Description: r.Description,
			Status:      r.Status,
			StartDate:   r.StartDate,
			EndDate:     r.EndDate,
		}
	}
	return projects, nil
}

// ProjectTransactions implements domain.ProjectLedger. It finds the financial
// year covering the from–to window, fetches all vouchers in that year, and
// returns rows where VoucherRow.Project == projectID.
func (a *ProjectLedgerAdapter) ProjectTransactions(ctx context.Context, tenantID domain.TenantID, projectID string, from, to time.Time) ([]domain.VoucherRow, error) {
	years, err := a.gl.FinancialYears(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("project ledger transactions: %w", err)
	}

	// Find first year that overlaps with the requested window.
	yearID := 0
	for _, y := range years {
		if !y.To.Before(from) && !y.From.After(to) {
			yearID = y.ID
			break
		}
	}
	if yearID == 0 {
		return nil, fmt.Errorf("project ledger transactions: no financial year found for range %s–%s", from.Format("2006-01-02"), to.Format("2006-01-02"))
	}

	vouchers, err := a.gl.Vouchers(ctx, tenantID, yearID, from, to)
	if err != nil {
		return nil, fmt.Errorf("project ledger transactions: %w", err)
	}

	var rows []domain.VoucherRow
	for _, v := range vouchers {
		for _, row := range v.Rows {
			if row.Project == projectID {
				rows = append(rows, row)
			}
		}
	}
	return rows, nil
}
