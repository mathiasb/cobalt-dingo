package fortnox_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	adapterfortnox "github.com/mathiasb/cobalt-dingo/internal/adapter/fortnox"
	"github.com/mathiasb/cobalt-dingo/internal/config"
	"github.com/mathiasb/cobalt-dingo/internal/domain"
	"github.com/mathiasb/cobalt-dingo/internal/fortnox"
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

// TestProductionWiring_ERPWriterCannotWrite covers the money path itself.
//
// ERPWriter.RecordAndBookkeep is the only thing in this repo that POSTs and PUTs
// to Fortnox — it records a supplier-invoice payment and books the GL voucher.
// BuildMCPDeps does not construct it, so the test above never reached it.
//
// This matters more than it looks: Fortnox connected-app scopes are
// resource-scoped, not verb-scoped, so there is NO read-only scope to fall back
// on (see ADR-0001's 2026-09-08 amendment). In production the client-side gate
// is the only thing standing between this method and the live company's books.
func TestProductionWiring_ERPWriterCannotWrite(t *testing.T) {
	var (
		mu      sync.Mutex
		methods []string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		methods = append(methods, r.Method)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	cfg := config.Fortnox{
		Mode:            config.ModeProduction,
		AllowsWrites:    false,
		BaseURLOverride: srv.URL,
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	writer := adapterfortnox.NewERPWriter(adapterfortnox.NewConnector(cfg, newStubTokenStore(), log))

	err := writer.RecordAndBookkeep(
		context.Background(),
		domain.TenantID("test"),
		domain.BatchItem{FortnoxInvoiceNumber: 1234, Amount: domain.Money{MinorUnits: 10000, Currency: "EUR"}},
		11.42,
		"2026-09-08",
	)

	require.Error(t, err, "recording a payment against the live company must fail")
	assert.ErrorIs(t, err, fortnox.ErrReadOnlyClient,
		"it must fail at the read-only gate, not incidentally on a malformed response")

	mu.Lock()
	defer mu.Unlock()
	for _, m := range methods {
		assert.Equal(t, http.MethodGet, m,
			"no write request may reach Fortnox in production — ADR-0001 violated")
	}
}
