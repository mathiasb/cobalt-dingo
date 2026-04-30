package fortnox

import (
	"context"
	"fmt"

	"github.com/mathiasb/cobalt-dingo/internal/domain"
	rawfortnox "github.com/mathiasb/cobalt-dingo/internal/fortnox"
)

// CompanyInfoAdapter implements domain.CompanyInfo using the Fortnox REST API.
type CompanyInfoAdapter struct {
	baseURL  string
	tokens   domain.TokenStore
	readOnly bool
}

// NewCompanyInfoAdapter returns a CompanyInfoAdapter pointed at baseURL
// and backed by the given token store. readOnly is propagated to the raw
// Fortnox client.
func NewCompanyInfoAdapter(baseURL string, tokens domain.TokenStore, readOnly bool) *CompanyInfoAdapter {
	return &CompanyInfoAdapter{baseURL: baseURL, tokens: tokens, readOnly: readOnly}
}

// client loads the tenant's access token and returns a ready-to-use raw Fortnox client.
func (a *CompanyInfoAdapter) client(ctx context.Context, tenantID domain.TenantID) (*rawfortnox.Client, error) {
	tok, err := a.tokens.Load(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("load token: %w", err)
	}
	return rawfortnox.NewClient(a.baseURL, tok.AccessToken, a.readOnly), nil
}

// Info implements domain.CompanyInfo.
func (a *CompanyInfoAdapter) Info(ctx context.Context, tenantID domain.TenantID) (domain.Company, error) {
	c, err := a.client(ctx, tenantID)
	if err != nil {
		return domain.Company{}, fmt.Errorf("company info: %w", err)
	}

	row, err := c.GetCompanyInfo()
	if err != nil {
		return domain.Company{}, fmt.Errorf("company info: %w", err)
	}

	return domain.Company{
		Name:         row.CompanyName,
		OrgNumber:    row.OrganizationNumber,
		Address:      row.Address,
		City:         row.City,
		ZipCode:      row.ZipCode,
		Country:      row.Country,
		Email:        row.Email,
		Phone:        row.Phone,
		VisitAddress: row.VisitAddress,
		VisitCity:    row.VisitCity,
		VisitZipCode: row.VisitZipCode,
	}, nil
}
