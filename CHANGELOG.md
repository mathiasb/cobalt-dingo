# Changelog

All notable changes to cobalt-dingo are documented here.

Format follows [Keep a Changelog](https://keepachangelog.com/en/1.0.0/).
This project uses [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

Pre-1.0 minor bumps (`0.x`) signal meaningful milestones. Breaking changes to
the domain model or port interfaces will be called out explicitly.

---

## [Unreleased]

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
