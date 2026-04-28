# Changelog

All notable changes to cobalt-dingo are documented here.

Format follows [Keep a Changelog](https://keepachangelog.com/en/1.0.0/).
This project uses [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

Pre-1.0 minor bumps (`0.x`) signal meaningful milestones. Breaking changes to
the domain model or port interfaces will be called out explicitly.

---

## [Unreleased]

---

## [0.5.1] — 2026-04-28

GitOps deploy + sandbox seed buildout. No user-facing behavior change;
release covers infra and test-data plumbing for the v0.5.0 surfaces.

### Changed

- **CI deploy** runs through Flux instead of direct `kubectl apply` /
  `set image`. Job clones `gitea.d-ma.be/mathias/infra`, sed-patches
  `k3s/apps/cobalt-dingo/deployment.yaml`, commits and pushes; Flux
  reconciles within ~10s. Cluster state is now declared in the infra
  repo and auto-corrects manual drift. See `DECISIONS.md` 2026-04-27.
- **Fortnox OAuth scopes** extended to cover the v0.5.0 surfaces:
  `companyinformation`, `bookkeeping`, `customer`, `invoice`, `supplier`,
  `supplierinvoice`, `payment`, `currency`, `project`, `costcenter`,
  `assets`, `settings`. Drops `inbox` (unused).
- **Fortnox client rate limiter** moved from per-method opt-in to a
  `Client.do(req)` wrapper used by every request. Cap dialled to
  18 req/5 s — Fortnox's documented 25/5 s appears to use a sliding
  window that 429s under bursts.
- **Deploy verification** dropped `sudo k3s kubectl` in favour of plain
  `kubectl` (the runner has a working `~/.kube/config`).

### Added

- `cmd/probe-sandbox` — checks all 18 Fortnox endpoints we care about
  and reports record counts.
- `cmd/e2e-seed` extended with five customers (NO/DE/FI/SE), ten
  customer invoices spanning every aging bucket (paid ×2, current ×3,
  overdue 1–30/31–60/90+), three projects (ongoing ×2, completed),
  two cost centers (ENG, SALES), one fixed asset (computer, mid-
  depreciation).
- New `internal/fortnox` CRUD: `CreateCustomer`, `SetCustomerActive`,
  `ListCustomers`, `CreateCustomerInvoice`, `BookkeepCustomerInvoice`,
  `CancelCustomerInvoice`, `FullyPayCustomerInvoice`, `CreateProject`,
  `SetProjectStatus`, `ListProjectsByPrefix`, `CreateCostCenter`,
  `SetCostCenterActive`, `ListCostCentersByPrefix`, `CreateAsset`,
  `ListAssetsByPrefix`.

### Notes for next session

- `cmd/e2e-teardown` still only deactivates suppliers — extending
  it to cover the new entity types is the natural next step.
- Asset POST quirks: response wrapper is `Assets` (plural) even
  though the request wrapper is `Asset` (singular); `AcquisitionStart`
  must be the 1st of a month; the field name is mistranslated as
  "Avskrivningsstart" in error messages.

---

## [0.5.0] — 2026-04-25

Financial command center milestone. Conversational analytics layer across six
Fortnox ledgers, exposed via MCP (38 tools) and an HTMX chat fallback UI.

### Added
- **Domain layer** — 15 new types (`CustomerInvoice`, `Account`, `Voucher`, `Project`,
  `CostCenter`, `Asset`, `Company`, etc.) and 7 port interfaces (`SupplierLedger`,
  `CustomerLedger`, `GeneralLedger`, `ProjectLedger`, `CostCenterLedger`,
  `AssetRegister`, `CompanyInfo`)
- **Aggregation library** (`internal/analyst/`) — `AgingBuckets`, `GroupBy`,
  `SumMinorUnits`, `DaysOverdue`, `OrderedKeys` with 13 table-driven tests
- **Fortnox client extensions** — generic `Get()` with rate limiting (25 req/5s),
  `GetAllPages()` for automatic pagination
- **7 Fortnox adapters** — supplier ledger, customer ledger, general ledger,
  project ledger, cost center ledger, asset register, company info; all tested
  with `httptest`
- **MCP server** (`cmd/mcp/`) — stdio transport, 38 tools:
  - 7 AP tools (summary, overdue, by-supplier, by-currency, aging, history, detail)
  - 7 AR tools (mirrors AP for customer invoices)
  - 7 GL tools (chart of accounts, balances, activity, vouchers, detail,
    predefined accounts, financial years)
  - 3 project tools (list, transactions, profitability)
  - 3 cost center tools (list, transactions, analysis)
  - 2 asset tools (list, detail)
  - 9 analytics tools (cash flow forecast, expense analysis, period comparison,
    yearly comparison, gross margin trend, top customers/suppliers,
    sales vs purchases, company info)
- **HTMX chat UI** (`GET /chat`) — Claude API with tool use, SSE streaming,
  Chart.js visualisations, conversation history
- **Taskfile** — `task mcp` and `task mcp:build` commands

### Changed
- `cmd/server/main.go` — wires chat handler when `ANTHROPIC_API_KEY` is set
- `internal/config/config.go` — added `Claude` config struct
- `go.mod` — added `mark3labs/mcp-go`, `stretchr/testify`

---

## [0.4.0] — 2026-04-17

Full pipeline milestone. The payment lifecycle is now end-to-end: detect → enrich
(cached) → generate → persist → submit → confirm → write-back.

### Added
- **A — PostgreSQL adapter** (`internal/adapter/postgres/`)
  - `Store` — shared pool, sqlc `Queries` wrapper
  - `TokenStore` — `AtomicRefresh` via `UPDATE WHERE refresh_token = $old`; zero rows → `ErrTokenConflict`
  - `BatchRepo` — transactional batch + items insert; `Get` loads items, `List` does not
  - `TenantRepo` — `Get` + `DefaultDebtorAccount`
  - `internal/adapter/postgres/pgstore/` — sqlc-generated query code; regenerate with `task db:sqlc`
  - `sqlc.yaml` config; `task db:sqlc` added to Taskfile
- **D — Supplier IBAN cache** (`adapter/fortnox/supplier_cache.go`)
  - `CachingEnricher` wraps any `SupplierEnricher` with per-(tenant, supplier) TTL map
  - `Invalidate()` for WebSocket `supplier-updated-v1` event-driven invalidation
  - Wired at 5-min TTL in `main.go`; per-request dedup in handler still present
- **B — PISP submission**
  - `domain.BatchService` — `SaveDraft`, `Submit` orchestrate draft → submitted lifecycle
  - `adapter/pisp.Stub` — no-op PaymentSubmitter that logs and returns a fake ref
  - `POST /invoices/batch/submit` handler + `SubmitConfirmation` templ component
  - Submit button live when `DATABASE_URL` is set; gracefully disabled otherwise
- **C — FX delta and ERP write-back**
  - `domain.CalculateFXDelta` — computes `(execRate − invRate) × minorUnits` in öre
  - `domain.BatchService.ConfirmExecution` — per-item ERPWriter calls; partial failure collection
  - `domain.ERPWriter` interface + `adapter/fortnox.ERPWriter` — `POST /3/supplierinvoicepayments` + `PUT .../bookkeep`
  - `fortnox.RecordPayment` + `BookkeepPayment` raw client methods

### Changed
- `cmd/server/main.go` — wires postgres adapters when `DATABASE_URL` is set; supplier cache in all live configurations
- `lib/pq` promoted from indirect to direct dependency

---

## [0.3.0] — 2026-04-16

Hexagonal architecture milestone. The domain layer is now fully decoupled from
all external systems. All I/O flows through explicit port interfaces.

### Added
- `internal/domain/ports.go` — `InvoiceSource`, `SupplierEnricher`, `BatchRepository`,
  `TenantRepository`, `TokenStore`, `PaymentSubmitter` port interfaces
- `internal/domain/batch.go` — `Batch`, `BatchItem`, `BatchStatus` domain types with
  lifecycle states: `draft → submitted → confirmed → reconciled`
- `internal/domain/tenant.go` — `TenantID`, `Tenant`, `DebtorAccount` (supports future
  PISP handle)
- `internal/domain/money.go` — `Money` fixed-point type (`int64` minor units + ISO 4217
  currency); replaces `float64` throughout the domain
- `internal/adapter/fortnox/connector.go` — `Connector` implements `InvoiceSource` +
  `SupplierEnricher`; handles token refresh with `AtomicRefresh` to prevent rolling-token
  race conditions
- `internal/adapter/file/tokenstore.go` — file-backed `TokenStore` with mutex-guarded CAS
  for single-process token management; production path for postgres adapter
- `internal/adapter/fake/` — stub `InvoiceSource` + `SupplierEnricher` for development
  without Fortnox credentials
- `migrations/001_initial.up.sql` + `down.sql` — initial schema: `tenants`,
  `debtor_accounts`, `fortnox_tokens`, `payment_batches`, `batch_items`; amounts stored as
  `BIGINT` minor units
- `cmd/migrate/main.go` — standalone migration runner using golang-migrate
- `OAuthToken.Valid()` on domain token type (30-second buffer)
- `task db:migrate` and `task db:migrate:down` in Taskfile

### Changed
- `internal/ui/handler.go` — `Server` now accepts `domain.InvoiceSource` +
  `domain.SupplierEnricher` ports instead of raw `config.Fortnox`; no fortnox import;
  all handlers propagate `context.Context`
- `cmd/server/main.go` — wires real or fake adapters at startup based on whether
  `FORTNOX_CLIENT_ID` is set
- Deleted `internal/invoice/` package — merged into `internal/domain/`

---

## [0.2.0] — 2026-04-15

Live Fortnox integration milestone. Real invoices replace fake data; e2e CI pipeline
runs against the Fortnox sandbox on every push.

### Added
- Fortnox OAuth2 flow (`cmd/fortnox-auth/`) — authorization code exchange, token
  persistence to `.fortnox-tokens.json`
- Supplier IBAN/BIC enrichment — `GET /3/suppliers/{n}` lookup before PAIN.001 assembly;
  invoices without IBAN are silently excluded from the batch
- Per-request supplier cache in `handler.go` — deduplicates API calls within a single
  batch operation
- `cmd/e2e-seed/` + `cmd/e2e-teardown/` — sandbox data management for CI
- Full e2e CI job: seed → run server → hit `/invoices` → verify batch XML → teardown
- Automatic token refresh on expiry (30-second buffer) with file persistence
- `fakeInvoices` fallback when `FORTNOX_CLIENT_ID` is unset (dev mode)
- Batch XML download endpoint (`GET /invoices/batch/download`)

### Changed
- UI batch panel redesigned with currency-grouped sections, collapsible XML preview,
  and download link
- `PendingInvoice` extended with `IBAN` and `BIC` fields
- `BatchSummary` and `BatchGroup` types extracted to support grouped rendering

---

## [0.1.0] — 2026-04-14

First working pipeline milestone. FCY invoice detection, PAIN.001 generation, and
a styled browser UI.

### Added
- PAIN.001 generation (`internal/payment/pain001.go`) — `pain.001.001.03` XML with
  per-currency `PmtInf` blocks; EUR blocks carry SEPA service level
- Invoice queue (`internal/domain/`) — FCY filter (`IsForeignCurrency`), `Queue`,
  `Sync`, `Enrich`
- HTMX + Templ UI — invoice list, `POST /invoices/batch` partial, download link
- SEB Green Design System styling (chlorophyll v4 components)
- Acceptance test suite using godog/Gherkin — 7 scenarios covering PAIN.001
  generation, FCY filtering, control sum accuracy, multi-currency grouping
- Fortnox connector (`internal/fortnox/client.go`) — `GET /3/supplierinvoices?filter=unpaid`
- golangci-lint v2 with revive, errcheck, gofumpt linters
- k3s deployment via CI on koala (local runner)
- Smoke test in CI: container health check on `/healthz`

---

## [0.0.1] — 2026-04-08

Initial project scaffold. Go module, Taskfile, Gitea CI skeleton, k3s deploy
pipeline, project context files.
