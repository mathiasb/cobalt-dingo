package fortnox_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	adapterfortnox "github.com/mathiasb/cobalt-dingo/internal/adapter/fortnox"
	"github.com/mathiasb/cobalt-dingo/internal/domain"
)

func TestAssetRegister_Assets(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer test-access-token", r.Header.Get("Authorization"))
		assert.Equal(t, "/3/assets", r.URL.Path)

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"Assets": []map[string]any{
				{
					"Id":                        1,
					"Number":                    "A001",
					"Description":               "MacBook Pro",
					"AcquisitionDate":           "2024-01-15",
					"AcquisitionValue":          25000.0,
					"DepreciationMethod":        "Straight-line",
					"DepreciateToResidualValue": 5.0,
					"BookValue":                 20000.0,
					"AccumulatedDepreciation":   5000.0,
				},
			},
		})
	}))
	defer srv.Close()

	adapter := adapterfortnox.NewAssetRegisterAdapter(srv.URL, &stubTokenStore{}, false)
	assets, err := adapter.Assets(context.Background(), domain.TenantID("t1"))

	require.NoError(t, err)
	require.Len(t, assets, 1)

	a := assets[0]
	assert.Equal(t, 1, a.ID)
	assert.Equal(t, "A001", a.Number)
	assert.Equal(t, "MacBook Pro", a.Description)
	assert.Equal(t, domain.MoneyFromFloat(20000.0, "SEK"), a.BookValue)
}

func TestAssetRegister_AssetDetail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer test-access-token", r.Header.Get("Authorization"))
		assert.Equal(t, "/3/assets/1", r.URL.Path)

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"Asset": map[string]any{
				"Id":                        1,
				"Number":                    "A001",
				"Description":               "MacBook Pro",
				"AcquisitionDate":           "2024-01-15",
				"AcquisitionValue":          25000.0,
				"DepreciationMethod":        "Straight-line",
				"DepreciateToResidualValue": 5.0,
				"BookValue":                 20000.0,
				"AccumulatedDepreciation":   5000.0,
			},
		})
	}))
	defer srv.Close()

	adapter := adapterfortnox.NewAssetRegisterAdapter(srv.URL, &stubTokenStore{}, false)
	a, err := adapter.AssetDetail(context.Background(), domain.TenantID("t1"), 1)

	require.NoError(t, err)
	assert.Equal(t, 1, a.ID)
	assert.Equal(t, "MacBook Pro", a.Description)
	assert.Equal(t, domain.MoneyFromFloat(20000.0, "SEK"), a.BookValue)
}
