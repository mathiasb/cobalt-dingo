//go:build integration

package mcp_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mathiasb/cobalt-dingo/internal/adapter/file"
	adapterfortnox "github.com/mathiasb/cobalt-dingo/internal/adapter/fortnox"
	"github.com/mathiasb/cobalt-dingo/internal/config"
	"github.com/mathiasb/cobalt-dingo/internal/domain"
	mcpserver "github.com/mathiasb/cobalt-dingo/internal/mcp"
)

var testDeps mcpserver.Deps

func TestMain(m *testing.M) {
	if os.Getenv("FORTNOX_MODE") == "" {
		fmt.Println("SKIP: FORTNOX_MODE not set — run with FORTNOX_MODE=sandbox and .env sourced")
		os.Exit(0)
	}

	// go test sets cwd to the package dir; token files live at repo root.
	root, err := repoRoot()
	if err != nil {
		fmt.Fprintf(os.Stderr, "find repo root: %v\n", err)
		os.Exit(1)
	}
	if err := os.Chdir(root); err != nil {
		fmt.Fprintf(os.Stderr, "chdir %s: %v\n", root, err)
		os.Exit(1)
	}

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config.Load: %v\n", err)
		os.Exit(1)
	}

	tokenStore := file.NewTokenStore(cfg.Mode.TokenFile())
	baseURL := cfg.BaseURL()
	tenantID := domain.TenantID("default")
	readOnly := !cfg.Mode.AllowsWrites()

	gl := adapterfortnox.NewGeneralLedgerAdapter(baseURL, tokenStore, readOnly)

	testDeps = mcpserver.Deps{
		TenantID:    tenantID,
		SupplierLdg: adapterfortnox.NewSupplierLedgerAdapter(baseURL, tokenStore, readOnly),
		CustomerLdg: adapterfortnox.NewCustomerLedgerAdapter(baseURL, tokenStore, readOnly),
		GeneralLdg:  gl,
		ProjectLdg:  adapterfortnox.NewProjectLedgerAdapter(baseURL, tokenStore, gl, readOnly),
		CostCtrLdg:  adapterfortnox.NewCostCenterLedgerAdapter(baseURL, tokenStore, gl, readOnly),
		AssetReg:    adapterfortnox.NewAssetRegisterAdapter(baseURL, tokenStore, readOnly),
		CompanyInf:  adapterfortnox.NewCompanyInfoAdapter(baseURL, tokenStore, readOnly),
	}

	os.Exit(m.Run())
}

func callTool(t *testing.T, name string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	handler := mcpserver.DispatchTool(name, testDeps)
	if handler == nil {
		t.Fatalf("no handler registered for tool %q", name)
	}
	req := mcp.CallToolRequest{}
	req.Params.Name = name
	req.Params.Arguments = args
	// 300ms between calls keeps burst rate well below Fortnox's 25 req/5s limit.
	time.Sleep(300 * time.Millisecond)
	res, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("%s: unexpected Go error: %v", name, err)
	}
	if res == nil {
		t.Fatalf("%s: nil result", name)
	}
	return res
}

func resultText(t *testing.T, name string, res *mcp.CallToolResult) string {
	t.Helper()
	if res.IsError {
		var msg string
		for _, c := range res.Content {
			if tc, ok := c.(mcp.TextContent); ok {
				msg = tc.Text
			}
		}
		t.Errorf("%s: tool returned error result: %s", name, msg)
		return ""
	}
	for _, c := range res.Content {
		if tc, ok := c.(mcp.TextContent); ok {
			return tc.Text
		}
	}
	t.Errorf("%s: no text content in result", name)
	return ""
}

func callOK(t *testing.T, name string, args map[string]any) string {
	t.Helper()
	return resultText(t, name, callTool(t, name, args))
}

func TestSmokeAllTools(t *testing.T) {
	const (
		from = "2025-01-01"
		to   = "2026-12-31"
	)

	// ── AP: no-param ────────────────────────────────────────────────────────

	t.Run("ap_summary", func(t *testing.T) { callOK(t, "ap_summary", nil) })
	t.Run("ap_by_currency", func(t *testing.T) { callOK(t, "ap_by_currency", nil) })
	t.Run("ap_aging", func(t *testing.T) { callOK(t, "ap_aging", nil) })

	// ap_by_supplier — also used to discover supplier_number
	apBySupplierText := callOK(t, "ap_by_supplier", nil)
	var apSupplierGroups []struct {
		SupplierNumber int `json:"supplier_number"`
	}
	mustUnmarshal(t, "ap_by_supplier", apBySupplierText, &apSupplierGroups)

	// ap_supplier_history — discover invoice_number for ap_invoice_detail
	t.Run("ap_supplier_history", func(t *testing.T) {
		if len(apSupplierGroups) == 0 {
			t.Skip("no suppliers with unpaid invoices")
		}
		snum := apSupplierGroups[0].SupplierNumber

		histText := callOK(t, "ap_supplier_history", map[string]any{
			"supplier_number": float64(snum),
		})

		// use the first invoice from history for ap_invoice_detail
		var hist struct {
			Invoices []struct {
				InvoiceNumber int `json:"InvoiceNumber"`
			} `json:"invoices"`
		}
		if err := json.Unmarshal([]byte(histText), &hist); err == nil && len(hist.Invoices) > 0 {
			t.Run("ap_invoice_detail", func(t *testing.T) {
				callOK(t, "ap_invoice_detail", map[string]any{
					"invoice_number": float64(hist.Invoices[0].InvoiceNumber),
				})
			})
		}
	})

	t.Run("ap_overdue", func(t *testing.T) { callOK(t, "ap_overdue", nil) })

	// ── AR: no-param ────────────────────────────────────────────────────────

	t.Run("ar_summary", func(t *testing.T) { callOK(t, "ar_summary", nil) })
	t.Run("ar_aging", func(t *testing.T) { callOK(t, "ar_aging", nil) })
	t.Run("ar_unpaid_report", func(t *testing.T) { callOK(t, "ar_unpaid_report", nil) })
	t.Run("ar_overdue", func(t *testing.T) { callOK(t, "ar_overdue", nil) })

	// ar_by_customer — discover customer_number
	arByCustomerText := callOK(t, "ar_by_customer", nil)
	var arCustomerGroups []struct {
		CustomerNumber int `json:"customer_number"`
	}
	mustUnmarshal(t, "ar_by_customer", arByCustomerText, &arCustomerGroups)

	// ar_customer_history — discover invoice_number for ar_invoice_detail
	t.Run("ar_customer_history", func(t *testing.T) {
		if len(arCustomerGroups) == 0 {
			t.Skip("no customers with unpaid invoices")
		}
		cnum := arCustomerGroups[0].CustomerNumber

		histText := callOK(t, "ar_customer_history", map[string]any{
			"customer_number": float64(cnum),
		})

		var hist struct {
			Invoices []struct {
				InvoiceNumber int `json:"InvoiceNumber"`
			} `json:"invoices"`
		}
		if err := json.Unmarshal([]byte(histText), &hist); err == nil && len(hist.Invoices) > 0 {
			t.Run("ar_invoice_detail", func(t *testing.T) {
				callOK(t, "ar_invoice_detail", map[string]any{
					"invoice_number": float64(hist.Invoices[0].InvoiceNumber),
				})
			})
		}
	})

	// ── GL ───────────────────────────────────────────────────────────────────

	t.Run("gl_chart_of_accounts", func(t *testing.T) { callOK(t, "gl_chart_of_accounts", nil) })
	t.Run("gl_predefined_accounts", func(t *testing.T) { callOK(t, "gl_predefined_accounts", nil) })

	t.Run("gl_account_balance", func(t *testing.T) {
		callOK(t, "gl_account_balance", map[string]any{
			"account_from": float64(1000),
			"account_to":   float64(9999),
		})
	})

	t.Run("gl_account_activity", func(t *testing.T) {
		callOK(t, "gl_account_activity", map[string]any{
			"account_num": float64(3000),
			"from_date":   from,
			"to_date":     to,
		})
	})

	// gl_financial_years — discover year IDs and date bounds
	glYearsText := callOK(t, "gl_financial_years", nil)
	var glYears []struct {
		ID   int    `json:"ID"`
		From string `json:"From"`
		To   string `json:"To"`
	}
	mustUnmarshal(t, "gl_financial_years", glYearsText, &glYears)

	t.Run("yearly_comparison", func(t *testing.T) {
		if len(glYears) < 2 {
			t.Skip("need at least 2 financial years")
		}
		callOK(t, "yearly_comparison", map[string]any{
			"year1": float64(glYears[0].ID),
			"year2": float64(glYears[1].ID),
		})
	})

	// gl_vouchers — also discover voucher for gl_voucher_detail
	glVouchersText := callOK(t, "gl_vouchers", map[string]any{
		"from_date": from,
		"to_date":   to,
	})
	var glVouchers []struct {
		Series string `json:"Series"`
		Number int    `json:"Number"`
	}
	mustUnmarshal(t, "gl_vouchers", glVouchersText, &glVouchers)

	t.Run("gl_voucher_detail", func(t *testing.T) {
		if len(glVouchers) == 0 {
			t.Skip("no vouchers in date range")
		}
		callOK(t, "gl_voucher_detail", map[string]any{
			"series": glVouchers[0].Series,
			"number": float64(glVouchers[0].Number),
		})
	})

	// ── Projects ─────────────────────────────────────────────────────────────

	projectListText := callOK(t, "project_list", nil)
	var projects []struct {
		Number string `json:"Number"`
	}
	mustUnmarshal(t, "project_list", projectListText, &projects)

	t.Run("project_transactions", func(t *testing.T) {
		if len(projects) == 0 {
			t.Skip("no projects")
		}
		callOK(t, "project_transactions", map[string]any{
			"project_id": projects[0].Number,
			"from_date":  from,
			"to_date":    to,
		})
	})

	t.Run("project_profitability", func(t *testing.T) {
		if len(projects) == 0 {
			t.Skip("no projects")
		}
		callOK(t, "project_profitability", map[string]any{
			"project_id": projects[0].Number,
		})
	})

	// ── Cost Centers ─────────────────────────────────────────────────────────

	ccListText := callOK(t, "costcenter_list", nil)
	var costCenters []struct {
		Code string `json:"Code"`
	}
	mustUnmarshal(t, "costcenter_list", ccListText, &costCenters)

	t.Run("costcenter_transactions", func(t *testing.T) {
		if len(costCenters) == 0 {
			t.Skip("no cost centers")
		}
		if len(glYears) == 0 {
			t.Skip("no financial years")
		}
		// Use the first financial year's actual bounds to avoid out-of-range errors.
		yearFrom := glYears[0].From[:10]
		yearTo := glYears[0].To[:10]
		callOK(t, "costcenter_transactions", map[string]any{
			"code":      costCenters[0].Code,
			"from_date": yearFrom,
			"to_date":   yearTo,
		})
	})

	t.Run("costcenter_analysis", func(t *testing.T) {
		if len(costCenters) == 0 {
			t.Skip("no cost centers")
		}
		callOK(t, "costcenter_analysis", map[string]any{
			"code": costCenters[0].Code,
		})
	})

	// ── Assets ───────────────────────────────────────────────────────────────

	assetListText := callOK(t, "asset_list", nil)
	var assets []struct {
		ID int `json:"ID"`
	}
	mustUnmarshal(t, "asset_list", assetListText, &assets)

	t.Run("asset_detail", func(t *testing.T) {
		if len(assets) == 0 {
			t.Skip("no assets")
		}
		callOK(t, "asset_detail", map[string]any{
			"asset_id": float64(assets[0].ID),
		})
	})

	// ── Analytics ────────────────────────────────────────────────────────────

	t.Run("cash_flow_forecast", func(t *testing.T) { callOK(t, "cash_flow_forecast", nil) })
	t.Run("gross_margin_trend", func(t *testing.T) { callOK(t, "gross_margin_trend", nil) })
	t.Run("company_info", func(t *testing.T) { callOK(t, "company_info", nil) })

	t.Run("expense_analysis", func(t *testing.T) {
		callOK(t, "expense_analysis", map[string]any{
			"from_date": from,
			"to_date":   to,
		})
	})

	t.Run("period_comparison", func(t *testing.T) {
		callOK(t, "period_comparison", map[string]any{
			"period1_from": "2025-01-01",
			"period1_to":   "2025-06-30",
			"period2_from": "2025-07-01",
			"period2_to":   "2025-12-31",
		})
	})

	t.Run("top_customers", func(t *testing.T) {
		callOK(t, "top_customers", map[string]any{
			"from_date": from,
			"to_date":   to,
		})
	})

	t.Run("top_suppliers", func(t *testing.T) {
		callOK(t, "top_suppliers", map[string]any{
			"from_date": from,
			"to_date":   to,
		})
	})

	t.Run("sales_vs_purchases", func(t *testing.T) {
		callOK(t, "sales_vs_purchases", map[string]any{
			"from_date": from,
			"to_date":   to,
		})
	})
}

func repoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("go.mod not found traversing up from %s", dir)
		}
		dir = parent
	}
}

func mustUnmarshal(t *testing.T, tool, text string, v any) {
	t.Helper()
	if text == "" {
		return
	}
	if !strings.HasPrefix(strings.TrimSpace(text), "[") && !strings.HasPrefix(strings.TrimSpace(text), "{") {
		return
	}
	if err := json.Unmarshal([]byte(text), v); err != nil {
		t.Logf("%s: could not unmarshal for discovery: %v (text: %.200s)", tool, err, text)
	}
}
