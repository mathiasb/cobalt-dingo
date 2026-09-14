package fortnox

import (
	"encoding/json"
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
	return listAll(c, c.baseURL+"/3/projects", "projects",
		func(raw json.RawMessage) ([]ProjectRow, error) {
			return decodeInto(raw, func(e ProjectsResponse) []ProjectRow { return e.Projects })
		})
}
