// Package mcp provides the MCP server and tool handlers for cobalt-dingo.
package mcp

import (
	"github.com/mark3labs/mcp-go/server"
	"github.com/mathiasb/cobalt-dingo/internal/domain"
)

// DispatchTool returns the ToolHandlerFunc for the named tool, or nil if unknown.
// Used by the chat handler to execute tools locally without going through the MCP transport.
func DispatchTool(name string, deps Deps) server.ToolHandlerFunc {
	dispatch := map[string]server.ToolHandlerFunc{
		// AP
		"ap_summary":          apSummaryHandler(deps),
		"ap_overdue":          apOverdueHandler(deps),
		"ap_by_supplier":      apBySupplierHandler(deps),
		"ap_by_currency":      apByCurrencyHandler(deps),
		"ap_aging":            apAgingHandler(deps),
		"ap_supplier_history": apSupplierHistoryHandler(deps),
		"ap_invoice_detail":   apInvoiceDetailHandler(deps),
		// AR
		"ar_summary":          arSummaryHandler(deps),
		"ar_overdue":          arOverdueHandler(deps),
		"ar_by_customer":      arByCustomerHandler(deps),
		"ar_aging":            arAgingHandler(deps),
		"ar_customer_history": arCustomerHistoryHandler(deps),
		"ar_invoice_detail":   arInvoiceDetailHandler(deps),
		"ar_unpaid_report":    arUnpaidReportHandler(deps),
		// GL
		"gl_chart_of_accounts":   glChartOfAccountsHandler(deps),
		"gl_account_balance":     glAccountBalanceHandler(deps),
		"gl_account_activity":    glAccountActivityHandler(deps),
		"gl_vouchers":            glVouchersHandler(deps),
		"gl_voucher_detail":      glVoucherDetailHandler(deps),
		"gl_predefined_accounts": glPredefinedAccountsHandler(deps),
		"gl_financial_years":     glFinancialYearsHandler(deps),
		// Projects
		"project_list":          projectListHandler(deps),
		"project_transactions":  projectTransactionsHandler(deps),
		"project_profitability": projectProfitabilityHandler(deps),
		// Cost centers
		"costcenter_list":         costCenterListHandler(deps),
		"costcenter_transactions": costCenterTransactionsHandler(deps),
		"costcenter_analysis":     costCenterAnalysisHandler(deps),
		// Assets
		"asset_list":   assetListHandler(deps),
		"asset_detail": assetDetailHandler(deps),
		// Analytics
		"cash_flow_forecast": cashFlowForecastHandler(deps),
		"expense_analysis":   expenseAnalysisHandler(deps),
		"period_comparison":  periodComparisonHandler(deps),
		"yearly_comparison":  yearlyComparisonHandler(deps),
		"gross_margin_trend": grossMarginTrendHandler(deps),
		"top_customers":      topCustomersHandler(deps),
		"top_suppliers":      topSuppliersHandler(deps),
		"sales_vs_purchases": salesVsPurchasesHandler(deps),
		"company_info":       companyInfoHandler(deps),
	}
	return dispatch[name]
}

// Deps holds all ledger dependencies the MCP tools need.
type Deps struct {
	TenantID    domain.TenantID
	SupplierLdg domain.SupplierLedger
	CustomerLdg domain.CustomerLedger
	GeneralLdg  domain.GeneralLedger
	ProjectLdg  domain.ProjectLedger
	CostCtrLdg  domain.CostCenterLedger
	AssetReg    domain.AssetRegister
	CompanyInf  domain.CompanyInfo
}

// NewServer creates an MCP server with all tools registered.
func NewServer(deps Deps) *server.MCPServer {
	s := server.NewMCPServer(
		"cobalt-dingo",
		"0.5.0",
		server.WithToolCapabilities(true),
	)
	registerAPTools(s, deps)
	registerARTools(s, deps)
	registerGLTools(s, deps)
	registerProjectTools(s, deps)
	registerCostCtrTools(s, deps)
	registerAssetTools(s, deps)
	registerAnalyticsTools(s, deps)
	return s
}
