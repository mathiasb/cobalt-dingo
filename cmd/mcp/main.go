// Package main is the cobalt-dingo MCP server entry point.
package main

import (
	"log/slog"
	"os"

	"github.com/mark3labs/mcp-go/server"
	"github.com/mathiasb/cobalt-dingo/internal/adapter/file"
	adapterfortnox "github.com/mathiasb/cobalt-dingo/internal/adapter/fortnox"
	"github.com/mathiasb/cobalt-dingo/internal/config"
	"github.com/mathiasb/cobalt-dingo/internal/domain"
	mcpserver "github.com/mathiasb/cobalt-dingo/internal/mcp"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stderr, nil))

	cfg, err := config.Load()
	if err != nil {
		log.Error("fortnox config required", "err", err)
		os.Exit(1)
	}

	tokenStore := file.NewTokenStore()
	baseURL := cfg.BaseURL()
	tenantID := domain.TenantID("default")

	gl := adapterfortnox.NewGeneralLedgerAdapter(baseURL, tokenStore)

	deps := mcpserver.Deps{
		TenantID:    tenantID,
		SupplierLdg: adapterfortnox.NewSupplierLedgerAdapter(baseURL, tokenStore),
		CustomerLdg: adapterfortnox.NewCustomerLedgerAdapter(baseURL, tokenStore),
		GeneralLdg:  gl,
		ProjectLdg:  adapterfortnox.NewProjectLedgerAdapter(baseURL, tokenStore, gl),
		CostCtrLdg:  adapterfortnox.NewCostCenterLedgerAdapter(baseURL, tokenStore, gl),
		AssetReg:    adapterfortnox.NewAssetRegisterAdapter(baseURL, tokenStore),
		CompanyInf:  adapterfortnox.NewCompanyInfoAdapter(baseURL, tokenStore),
	}

	s := mcpserver.NewServer(deps)

	log.Info("cobalt-dingo MCP server starting", "transport", "stdio")
	if err := server.ServeStdio(s); err != nil {
		log.Error("MCP server failed", "err", err)
		os.Exit(1)
	}
}
