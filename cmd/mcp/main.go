// Package main is the cobalt-dingo MCP server entry point.
package main

import (
	"fmt"
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

	// Banner goes to stderr because stdout carries the MCP framing.
	fmt.Fprintf(os.Stderr, "\n  cobalt-dingo MCP — mode: %s | token: %s | writes: %v\n\n",
		cfg.Mode.Label(), cfg.Mode.TokenFile(), cfg.Mode.AllowsWrites())

	tokenStore := file.NewTokenStore(cfg.Mode.TokenFile())
	baseURL := cfg.BaseURL()
	tenantID := domain.TenantID("default")
	readOnly := !cfg.Mode.AllowsWrites()

	gl := adapterfortnox.NewGeneralLedgerAdapter(baseURL, tokenStore, readOnly)

	deps := mcpserver.Deps{
		TenantID:    tenantID,
		SupplierLdg: adapterfortnox.NewSupplierLedgerAdapter(baseURL, tokenStore, readOnly),
		CustomerLdg: adapterfortnox.NewCustomerLedgerAdapter(baseURL, tokenStore, readOnly),
		GeneralLdg:  gl,
		ProjectLdg:  adapterfortnox.NewProjectLedgerAdapter(baseURL, tokenStore, gl, readOnly),
		CostCtrLdg:  adapterfortnox.NewCostCenterLedgerAdapter(baseURL, tokenStore, gl, readOnly),
		AssetReg:    adapterfortnox.NewAssetRegisterAdapter(baseURL, tokenStore, readOnly),
		CompanyInf:  adapterfortnox.NewCompanyInfoAdapter(baseURL, tokenStore, readOnly),
	}

	s := mcpserver.NewServer(deps)

	log.Info("cobalt-dingo MCP server starting", "transport", "stdio", "mode", cfg.Mode)
	if err := server.ServeStdio(s); err != nil {
		log.Error("MCP server failed", "err", err)
		os.Exit(1)
	}
}
