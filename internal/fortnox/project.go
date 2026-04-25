package fortnox

import (
	"encoding/json"
	"fmt"
)

// ProjectRow is the Fortnox JSON for a project from GET /3/projects.
type ProjectRow struct {
	ProjectNumber string `json:"ProjectNumber"`
	Description   string `json:"Description"`
	Status        string `json:"Status"`
	StartDate     string `json:"StartDate"`
	EndDate       string `json:"EndDate"`
}

// ProjectsResponse is the top-level envelope for GET /3/projects.
type ProjectsResponse struct {
	Projects []ProjectRow `json:"Projects"`
}

// ListProjects returns all projects for the company.
// Calls GET /3/projects.
func (c *Client) ListProjects() ([]ProjectRow, error) {
	body, err := c.Get(c.baseURL + "/3/projects")
	if err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}
	var envelope ProjectsResponse
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("decode projects: %w", err)
	}
	return envelope.Projects, nil
}
