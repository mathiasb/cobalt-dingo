package mcp

import (
	"github.com/mark3labs/mcp-go/server"
	"github.com/mathiasb/cobalt-dingo/internal/domain"
)

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
	return s
}
