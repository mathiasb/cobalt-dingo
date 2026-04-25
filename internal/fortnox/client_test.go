package fortnox

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetAllPages(t *testing.T) {
	var callCount atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)

		page := r.URL.Query().Get("page")
		if page == "" {
			page = "1"
		}

		w.Header().Set("Content-Type", "application/json")

		switch page {
		case "1":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"MetaInformation": map[string]any{
					"@TotalPages":     2,
					"@CurrentPage":    1,
					"@TotalResources": 3,
				},
				"Items": []map[string]any{
					{"id": 1},
					{"id": 2},
				},
			})
		case "2":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"MetaInformation": map[string]any{
					"@TotalPages":     2,
					"@CurrentPage":    2,
					"@TotalResources": 3,
				},
				"Items": []map[string]any{
					{"id": 3},
				},
			})
		default:
			http.Error(w, "unexpected page", http.StatusBadRequest)
		}
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "test-token")
	pages, err := c.GetAllPages(srv.URL + "/3/items")

	require.NoError(t, err)
	assert.Len(t, pages, 2, "expected 2 pages")
	assert.Equal(t, int32(2), callCount.Load(), "expected exactly 2 HTTP calls")
}

func TestGet(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
		assert.Equal(t, "application/json", r.Header.Get("Accept"))

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "test-token")
	raw, err := c.Get(srv.URL + "/3/something")

	require.NoError(t, err)
	require.NotNil(t, raw)

	var result map[string]any
	require.NoError(t, json.Unmarshal(raw, &result))
	assert.Equal(t, true, result["ok"])
}
