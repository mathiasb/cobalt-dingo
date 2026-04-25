package fortnox

import (
	"encoding/json"
	"fmt"
)

// CompanyInfoRow is the Fortnox JSON representation of company information.
type CompanyInfoRow struct {
	CompanyName        string `json:"CompanyName"`
	OrganizationNumber string `json:"OrganizationNumber"`
	Address            string `json:"Address"`
	City               string `json:"City"`
	ZipCode            string `json:"ZipCode"`
	Country            string `json:"Country"`
	Email              string `json:"Email"`
	Phone              string `json:"Phone1"`
	VisitAddress       string `json:"VisitAddress"`
	VisitCity          string `json:"VisitCity"`
	VisitZipCode       string `json:"VisitZipCode"`
}

// CompanyInfoResponse is the envelope for GET /3/companyinformation.
type CompanyInfoResponse struct {
	CompanyInformation CompanyInfoRow `json:"CompanyInformation"`
}

// GetCompanyInfo fetches the company profile.
// Calls GET /3/companyinformation.
func (c *Client) GetCompanyInfo() (CompanyInfoRow, error) {
	u := c.baseURL + "/3/companyinformation"
	raw, err := c.Get(u)
	if err != nil {
		return CompanyInfoRow{}, fmt.Errorf("get company info: %w", err)
	}
	var envelope CompanyInfoResponse
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return CompanyInfoRow{}, fmt.Errorf("decode company info: %w", err)
	}
	return envelope.CompanyInformation, nil
}
