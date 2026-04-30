package fortnox

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestReadOnlyClient_RejectsWritesLocally verifies the safety-critical
// write gate. A read-only client must refuse non-GET/HEAD requests before
// any HTTP traffic, returning ErrReadOnlyClient. This is defense in depth
// on top of the OAuth scope assigned to the Fortnox connected app.
func TestReadOnlyClient_RejectsWritesLocally(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "test-token", true) // readOnly=true

	for _, method := range []string{
		http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch,
	} {
		req, err := http.NewRequest(method, srv.URL+"/3/anything", nil)
		require.NoError(t, err)
		_, err = c.do(req)
		require.Error(t, err, "%s should be refused locally", method)
		assert.True(t, errors.Is(err, ErrReadOnlyClient),
			"%s error must wrap ErrReadOnlyClient: %v", method, err)
	}

	assert.Equal(t, int32(0), hits.Load(),
		"no HTTP traffic should reach the server when readOnly=true blocks writes")
}

// TestReadOnlyClient_AllowsReads verifies the gate doesn't over-trigger:
// GET and HEAD requests must still flow through to Fortnox.
func TestReadOnlyClient_AllowsReads(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "test-token", true)

	for _, method := range []string{http.MethodGet, http.MethodHead} {
		req, err := http.NewRequest(method, srv.URL+"/3/anything", nil)
		require.NoError(t, err)
		resp, err := c.do(req)
		require.NoError(t, err, "%s should be permitted", method)
		_ = resp.Body.Close()
	}

	assert.Equal(t, int32(2), hits.Load(),
		"GET and HEAD should both reach the server")
}

// TestWritableClient_AllowsWrites verifies that when readOnly=false the
// gate is dormant — non-GET requests pass through unchanged.
func TestWritableClient_AllowsWrites(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "test-token", false)

	req, err := http.NewRequest(http.MethodPost, srv.URL+"/3/anything", nil)
	require.NoError(t, err)
	resp, err := c.do(req)
	require.NoError(t, err)
	_ = resp.Body.Close()

	assert.Equal(t, int32(1), hits.Load())
}

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

	c := NewClient(srv.URL, "test-token", false)
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

	c := NewClient(srv.URL, "test-token", false)
	raw, err := c.Get(srv.URL + "/3/something")

	require.NoError(t, err)
	require.NotNil(t, raw)

	var result map[string]any
	require.NoError(t, json.Unmarshal(raw, &result))
	assert.Equal(t, true, result["ok"])
}
