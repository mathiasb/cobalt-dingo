package fortnox

import (
	"encoding/json"
	"fmt"
)

// AssetRow is the Fortnox JSON representation of a fixed asset.
type AssetRow struct {
	ID                        FlexInt `json:"Id"`
	Number                    string  `json:"Number"`
	Description               string  `json:"Description"`
	AcquisitionDate           string  `json:"AcquisitionDate"`
	AcquisitionValue          float64 `json:"AcquisitionValue"`
	DepreciationMethod        string  `json:"DepreciationMethod"`
	DepreciateToResidualValue float64 `json:"DepreciateToResidualValue"`
	BookValue                 float64 `json:"BookValue"`
	AccumulatedDepreciation   float64 `json:"AccumulatedDepreciation"`
}

// AssetsResponse is the envelope for GET /3/assets.
type AssetsResponse struct {
	Assets []AssetRow `json:"Assets"`
}

// AssetDetailResponse is the envelope for GET /3/assets/{id}.
type AssetDetailResponse struct {
	Asset AssetRow `json:"Asset"`
}

// ListAssets fetches all fixed assets from the asset register.
// Calls GET /3/assets.
func (c *Client) ListAssets() ([]AssetRow, error) {
	return listAll(c, c.baseURL+"/3/assets", "assets",
		func(raw json.RawMessage) ([]AssetRow, error) {
			return decodeInto(raw, func(e AssetsResponse) []AssetRow { return e.Assets })
		})
}

// GetAsset fetches a single fixed asset by ID.
// Calls GET /3/assets/{id}.
func (c *Client) GetAsset(assetID int) (AssetRow, error) {
	u := fmt.Sprintf("%s/3/assets/%d", c.baseURL, assetID)
	raw, err := c.get(u)
	if err != nil {
		return AssetRow{}, fmt.Errorf("get asset: %w", err)
	}
	var envelope AssetDetailResponse
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return AssetRow{}, fmt.Errorf("decode asset: %w", err)
	}
	return envelope.Asset, nil
}
