// Command probe-sandbox tests which Fortnox API endpoints are accessible
// in the current sandbox, to determine what test data we can seed.
//
// Usage:
//
//	source .env && go run ./cmd/probe-sandbox
package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"github.com/mathiasb/cobalt-dingo/internal/config"
	"github.com/mathiasb/cobalt-dingo/internal/fortnox"
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))

	cfg, err := config.Load()
	if err != nil {
		log.Error("config", "err", err)
		os.Exit(1)
	}

	token, err := loadValidToken(cfg, log)
	if err != nil {
		log.Error("token", "err", err)
		os.Exit(1)
	}

	baseURL := cfg.BaseURL()
	accessToken := token.AccessToken

	endpoints := []struct {
		name string
		path string
	}{
		{"Supplier invoices", "/3/supplierinvoices"},
		{"Customer invoices", "/3/invoices"},
		{"Customers", "/3/customers"},
		{"Suppliers", "/3/suppliers"},
		{"Accounts (chart)", "/3/accounts"},
		{"Vouchers", "/3/vouchers"},
		{"Financial years", "/3/financialyears"},
		{"Predefined accounts", "/3/predefinedaccounts"},
		{"Projects", "/3/projects"},
		{"Cost centers", "/3/costcenters"},
		{"Assets", "/3/assets"},
		{"Company info", "/3/companyinformation"},
		{"Currencies", "/3/currencies"},
		{"Invoice payments", "/3/invoicepayments"},
		{"Supplier payments", "/3/supplierinvoicepayments"},
		{"Orders", "/3/orders"},
		{"Offers", "/3/offers"},
		{"Articles", "/3/articles"},
	}

	fmt.Println("Probing Fortnox sandbox endpoints...")
	fmt.Println()
	fmt.Printf("%-25s %-8s %s\n", "ENDPOINT", "STATUS", "DETAIL")
	fmt.Println(strings.Repeat("─", 70))

	for _, ep := range endpoints {
		body, status := probe(baseURL+ep.path, accessToken)

		detail := ""
		if status == http.StatusOK {
			detail = summarize(body)
		} else {
			detail = extractError(body)
		}

		icon := "✓"
		if status != http.StatusOK {
			icon = "✗"
		}
		fmt.Printf("%-25s %s %-5d %s\n", ep.name, icon, status, detail)
	}

	fmt.Println()
}

func probe(url, token string) (json.RawMessage, int) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, 0
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, 0
	}
	defer func() { _ = resp.Body.Close() }()

	var body json.RawMessage
	_ = json.NewDecoder(resp.Body).Decode(&body)
	return body, resp.StatusCode
}

func summarize(body json.RawMessage) string {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(body, &obj); err != nil {
		return "(unparseable)"
	}

	if meta, ok := obj["MetaInformation"]; ok {
		var m struct {
			TotalResources int `json:"@TotalResources"`
		}
		if json.Unmarshal(meta, &m) == nil && m.TotalResources > 0 {
			return fmt.Sprintf("%d records", m.TotalResources)
		}
	}

	for key, val := range obj {
		if key == "MetaInformation" {
			continue
		}
		var arr []json.RawMessage
		if json.Unmarshal(val, &arr) == nil {
			return fmt.Sprintf("%d %s", len(arr), key)
		}
		var single map[string]any
		if json.Unmarshal(val, &single) == nil {
			if name, ok := single["CompanyName"]; ok {
				return fmt.Sprintf("company: %v", name)
			}
			return fmt.Sprintf("1 %s", key)
		}
	}

	return "ok"
}

func extractError(body json.RawMessage) string {
	var obj struct {
		ErrorInformation struct {
			Error   int    `json:"Error"`
			Message string `json:"Message"`
		} `json:"ErrorInformation"`
	}
	if json.Unmarshal(body, &obj) == nil && obj.ErrorInformation.Message != "" {
		return obj.ErrorInformation.Message
	}
	if len(body) > 100 {
		return string(body[:100]) + "..."
	}
	return string(body)
}

func loadValidToken(cfg config.Fortnox, log *slog.Logger) (fortnox.Token, error) {
	t, err := fortnox.LoadToken()
	if err != nil {
		return fortnox.Token{}, fmt.Errorf("no saved token — run fortnox-auth first: %w", err)
	}
	if t.Valid() {
		return t, nil
	}
	log.Info("access token expired, refreshing")
	t, err = fortnox.RefreshAccessToken(cfg.ClientID, cfg.ClientSecret, t.RefreshToken)
	if err != nil {
		return fortnox.Token{}, fmt.Errorf("refresh failed — re-run fortnox-auth: %w", err)
	}
	if err := fortnox.SaveToken(t); err != nil {
		log.Warn("could not save refreshed token", "err", err)
	}
	return t, nil
}
