package fortnox

import (
	"encoding/json"
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
	return listAll(c, c.baseURL+"/3/costcenters", "cost centers",
		func(raw json.RawMessage) ([]CostCenterRow, error) {
			return decodeInto(raw, func(e CostCentersResponse) []CostCenterRow { return e.CostCenters })
		})
}
