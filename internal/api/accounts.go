package api

import (
	"context"
	"fmt"
)

// Account represents a BAS account from Fortnox.
type Account struct {
	Number      int    `json:"Number"`
	Description string `json:"Description"`
	Active      bool   `json:"Active"`
	VATCode     string `json:"VATCode,omitempty"`
}

type accountsResponse struct {
	Accounts []Account `json:"Accounts"`
}

// ListAccounts fetches the full chart of accounts from Fortnox.
// Used by the validator to verify account numbers at startup.
func (c *Client) ListAccounts(ctx context.Context) ([]Account, error) {
	var resp accountsResponse
	if err := c.get(ctx, "accounts", &resp); err != nil {
		return nil, fmt.Errorf("api: list accounts: %w", err)
	}
	return resp.Accounts, nil
}
