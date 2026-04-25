package fortnox

import (
	"encoding/json"
	"fmt"
)

// CostCenterRow is a single cost center from the Fortnox API.
type CostCenterRow struct {
	Code        string `json:"Code"`
	Description string `json:"Description"`
	Active      bool   `json:"Active"`
}

// CostCentersResponse is the envelope for GET /3/costcenters.
type CostCentersResponse struct {
	CostCenters []CostCenterRow `json:"CostCenters"`
}

// ListCostCenters returns all cost centers.
// Calls GET /3/costcenters.
func (c *Client) ListCostCenters() ([]CostCenterRow, error) {
	body, err := c.Get(c.baseURL + "/3/costcenters")
	if err != nil {
		return nil, fmt.Errorf("list cost centers: %w", err)
	}
	var envelope CostCentersResponse
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("decode cost centers: %w", err)
	}
	return envelope.CostCenters, nil
}
