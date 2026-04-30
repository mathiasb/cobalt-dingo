package fortnox

import (
	"context"
	"fmt"

	"github.com/mathiasb/cobalt-dingo/internal/domain"
	rawfortnox "github.com/mathiasb/cobalt-dingo/internal/fortnox"
)

// AssetRegisterAdapter implements domain.AssetRegister using the Fortnox REST API.
type AssetRegisterAdapter struct {
	baseURL  string
	tokens   domain.TokenStore
	readOnly bool
}

// NewAssetRegisterAdapter returns an AssetRegisterAdapter pointed at
// baseURL and backed by the given token store. readOnly is propagated to
// the raw Fortnox client.
func NewAssetRegisterAdapter(baseURL string, tokens domain.TokenStore, readOnly bool) *AssetRegisterAdapter {
	return &AssetRegisterAdapter{baseURL: baseURL, tokens: tokens, readOnly: readOnly}
}

// client loads the tenant's access token and returns a ready-to-use raw Fortnox client.
func (a *AssetRegisterAdapter) client(ctx context.Context, tenantID domain.TenantID) (*rawfortnox.Client, error) {
	tok, err := a.tokens.Load(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("load token: %w", err)
	}
	return rawfortnox.NewClient(a.baseURL, tok.AccessToken, a.readOnly), nil
}

// rowToAsset converts a raw Fortnox AssetRow to a domain.Asset.
func rowToAsset(r rawfortnox.AssetRow) domain.Asset {
	return domain.Asset{
		ID:                  r.ID,
		Number:              r.Number,
		Description:         r.Description,
		AcquisitionDate:     r.AcquisitionDate,
		AcquisitionValue:    domain.MoneyFromFloat(r.AcquisitionValue, "SEK"),
		DepreciationMethod:  r.DepreciationMethod,
		DepreciationPercent: r.DepreciateToResidualValue,
		BookValue:           domain.MoneyFromFloat(r.BookValue, "SEK"),
		AccumulatedDepr:     domain.MoneyFromFloat(r.AccumulatedDepreciation, "SEK"),
	}
}

// Assets implements domain.AssetRegister.
func (a *AssetRegisterAdapter) Assets(ctx context.Context, tenantID domain.TenantID) ([]domain.Asset, error) {
	c, err := a.client(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("asset register: %w", err)
	}

	rows, err := c.ListAssets()
	if err != nil {
		return nil, fmt.Errorf("asset register: %w", err)
	}

	assets := make([]domain.Asset, len(rows))
	for i, row := range rows {
		assets[i] = rowToAsset(row)
	}
	return assets, nil
}

// AssetDetail implements domain.AssetRegister.
func (a *AssetRegisterAdapter) AssetDetail(ctx context.Context, tenantID domain.TenantID, assetID int) (domain.Asset, error) {
	c, err := a.client(ctx, tenantID)
	if err != nil {
		return domain.Asset{}, fmt.Errorf("asset register: %w", err)
	}

	row, err := c.GetAsset(assetID)
	if err != nil {
		return domain.Asset{}, fmt.Errorf("asset register: %w", err)
	}

	return rowToAsset(row), nil
}
