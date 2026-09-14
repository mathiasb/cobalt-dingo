package fortnox

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The structural audit proves these route through GetAllPages. These prove it
// works: a structural check can be satisfied by a call that is never reached.
func TestListsReadEveryPage(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		collection string
		fetch      func(*Client) (int, error)
	}{
		{
			name: "projects", path: "/3/projects", collection: "Projects",
			fetch: func(c *Client) (int, error) { r, err := c.ListProjects(); return len(r), err },
		},
		{
			name: "assets", path: "/3/assets", collection: "Assets",
			fetch: func(c *Client) (int, error) { r, err := c.ListAssets(); return len(r), err },
		},
		{
			name: "cost centers", path: "/3/costcenters", collection: "CostCenters",
			fetch: func(c *Client) (int, error) { r, err := c.ListCostCenters(); return len(r), err },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var requested []string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, tt.path, r.URL.Path)
				page := r.URL.Query().Get("page")
				if page == "" {
					page = "1"
				}
				requested = append(requested, page)
				// Three pages, one record each.
				_, _ = fmt.Fprintf(w,
					`{"MetaInformation":{"@CurrentPage":%s,"@TotalPages":3,"@TotalResources":3},"%s":[{}]}`,
					page, tt.collection)
			}))
			defer srv.Close()

			got, err := tt.fetch(NewClient(srv.URL, "tok", true))
			require.NoError(t, err)
			assert.Equal(t, 3, got, "one record from each of three pages")
			assert.Equal(t, []string{"1", "2", "3"}, requested)
		})
	}
}

// A page that will not decode must fail the call. Skipping it returns a short
// collection that looks complete — the defect the audit exists to prevent,
// arriving through a different door.
func TestListsRefuseAnUndecodablePage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") == "2" {
			_, _ = w.Write([]byte(`{"Projects": "not an array"}`))
			return
		}
		_, _ = w.Write([]byte(`{"MetaInformation":{"@CurrentPage":1,"@TotalPages":2,"@TotalResources":2},"Projects":[{}]}`))
	}))
	defer srv.Close()

	_, err := NewClient(srv.URL, "tok", true).ListProjects()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "page 2")
}
