package fortnox_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	adapterfortnox "github.com/mathiasb/cobalt-dingo/internal/adapter/fortnox"
	"github.com/mathiasb/cobalt-dingo/internal/config"
	"github.com/mathiasb/cobalt-dingo/internal/domain"
)

// TestProductionWiring_NeverYieldsWritableClient pins the invariant from
// ADR-0001: no production-mode code path may produce a write-capable Fortnox
// client.
//
// The client-side gate itself is already covered by
// internal/fortnox/client_test.go. What was untested — and what this covers —
// is that every adapter actually *receives* the read-only flag. Before this
// test, readOnly was a bare positional bool threaded by hand through eight
// constructors, in a block duplicated verbatim in cmd/server/main.go and
// cmd/mcp/main.go. Nothing in the type system distinguished a correct call from
// one that passed a literal, and nothing failed if a ninth adapter forgot the
// argument entirely.
//
// BuildMCPDeps exists so that block has exactly one home and derives readOnly
// from config rather than accepting it from a caller.
func TestProductionWiring_NeverYieldsWritableClient(t *testing.T) {
	var (
		mu      sync.Mutex
		methods []string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		methods = append(methods, r.Method)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"CompanyInformation":{}}`))
	}))
	defer srv.Close()

	tenant := domain.TenantID("test")
	production := config.Fortnox{
		Mode:            config.ModeProduction,
		AllowsWrites:    false,
		BaseURLOverride: srv.URL,
	}

	t.Run("production without writes builds deps that only ever GET", func(t *testing.T) {
		deps, err := adapterfortnox.BuildMCPDeps(production, newStubTokenStore(), tenant)
		require.NoError(t, err)
		require.NotNil(t, deps.CompanyInf, "every dependency must be wired")
		require.NotNil(t, deps.GeneralLdg)
		require.NotNil(t, deps.ProjectLdg)
		require.NotNil(t, deps.CostCtrLdg)
		require.NotNil(t, deps.SupplierLdg)
		require.NotNil(t, deps.CustomerLdg)
		require.NotNil(t, deps.AssetReg)
		assert.Equal(t, tenant, deps.TenantID)

		_, err = deps.CompanyInf.Info(context.Background(), tenant)
		require.NoError(t, err)

		mu.Lock()
		defer mu.Unlock()
		require.NotEmpty(t, methods, "the adapter must actually have called the server")
		for _, m := range methods {
			assert.Equal(t, http.MethodGet, m,
				"production wiring issued a non-GET request — ADR-0001 violated")
		}
	})

	t.Run("asking for a writable client in production is refused", func(t *testing.T) {
		writable := production
		writable.AllowsWrites = true

		_, err := adapterfortnox.BuildMCPDeps(writable, newStubTokenStore(), tenant)
		require.ErrorIs(t, err, adapterfortnox.ErrWritesInProduction,
			"opening the live write path requires an ADR that supersedes 0001, not a config flag")
	})

	t.Run("sandbox may still build writable deps", func(t *testing.T) {
		sandbox := production
		sandbox.Mode = config.ModeSandbox
		sandbox.AllowsWrites = true

		_, err := adapterfortnox.BuildMCPDeps(sandbox, newStubTokenStore(), tenant)
		require.NoError(t, err, "the sandbox is where writes are tested; this must not be blocked")
	})
}
