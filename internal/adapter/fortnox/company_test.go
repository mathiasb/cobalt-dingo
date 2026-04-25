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

func TestCompanyInfo_Info(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer test-access-token", r.Header.Get("Authorization"))
		assert.Equal(t, "/3/companyinformation", r.URL.Path)

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"CompanyInformation": map[string]any{
				"CompanyName":        "Test AB",
				"OrganizationNumber": "556677-8899",
				"Address":            "Storgatan 1",
				"City":               "Stockholm",
				"ZipCode":            "11122",
				"Country":            "Sweden",
				"Email":              "info@test.se",
				"Phone1":             "0812345678",
				"VisitAddress":       "Storgatan 1",
				"VisitCity":          "Stockholm",
				"VisitZipCode":       "11122",
			},
		})
	}))
	defer srv.Close()

	adapter := adapterfortnox.NewCompanyInfoAdapter(srv.URL, &stubTokenStore{})
	company, err := adapter.Info(context.Background(), domain.TenantID("t1"))

	require.NoError(t, err)
	assert.Equal(t, "Test AB", company.Name)
	assert.Equal(t, "556677-8899", company.OrgNumber)
	assert.Equal(t, "Storgatan 1", company.Address)
	assert.Equal(t, "Stockholm", company.City)
	assert.Equal(t, "11122", company.ZipCode)
	assert.Equal(t, "Sweden", company.Country)
	assert.Equal(t, "info@test.se", company.Email)
	assert.Equal(t, "0812345678", company.Phone)
	assert.Equal(t, "Storgatan 1", company.VisitAddress)
	assert.Equal(t, "Stockholm", company.VisitCity)
	assert.Equal(t, "11122", company.VisitZipCode)
}
