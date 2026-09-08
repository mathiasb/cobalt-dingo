package fortnox

import (
	"errors"
	"fmt"

	"github.com/mathiasb/cobalt-dingo/internal/config"
	"github.com/mathiasb/cobalt-dingo/internal/domain"
	mcpserver "github.com/mathiasb/cobalt-dingo/internal/mcp"
)

// ErrWritesInProduction is returned by BuildMCPDeps when asked to build
// write-capable dependencies against the live Fortnox company. Opening that
// path requires an ADR superseding docs/adr/0001, not a configuration change.
var ErrWritesInProduction = errors.New(
	"WRITE BLOCKED: refusing to build a write-capable Fortnox client in production mode (ADR-0001)")

// BuildMCPDeps assembles the full set of Fortnox-backed MCP dependencies from
// configuration. It is the single wiring point for these adapters: cmd/server
// and cmd/mcp both call it rather than repeating the construction.
//
// Read-only capability is derived here from cfg, never accepted from the
// caller. That is the point of the function — the adapters take readOnly as an
// untyped trailing bool, so a hand-written call site can silently get it wrong,
// and until this existed two of them repeated the same eight-constructor block
// verbatim. See TestProductionWiring_NeverYieldsWritableClient.
func BuildMCPDeps(
	cfg config.Fortnox,
	tokens domain.TokenStore,
	tenantID domain.TenantID,
) (mcpserver.Deps, error) {
	if cfg.Mode.IsProduction() && cfg.AllowsWrites {
		return mcpserver.Deps{}, fmt.Errorf("%w: mode=%s", ErrWritesInProduction, cfg.Mode)
	}

	readOnly := !cfg.AllowsWrites
	baseURL := cfg.BaseURL()

	gl := NewGeneralLedgerAdapter(baseURL, tokens, readOnly)

	return mcpserver.Deps{
		TenantID:    tenantID,
		SupplierLdg: NewSupplierLedgerAdapter(baseURL, tokens, readOnly),
		CustomerLdg: NewCustomerLedgerAdapter(baseURL, tokens, readOnly),
		GeneralLdg:  gl,
		ProjectLdg:  NewProjectLedgerAdapter(baseURL, tokens, gl, readOnly),
		CostCtrLdg:  NewCostCenterLedgerAdapter(baseURL, tokens, gl, readOnly),
		AssetReg:    NewAssetRegisterAdapter(baseURL, tokens, readOnly),
		CompanyInf:  NewCompanyInfoAdapter(baseURL, tokens, readOnly),
	}, nil
}
