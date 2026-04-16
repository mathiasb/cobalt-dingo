# cobalt-dingo

Automated foreign-currency AP payment pipeline for Nordic SMEs using Fortnox.

cobalt-dingo watches your Fortnox account for unpaid supplier invoices in foreign currencies (EUR, USD, GBP, CHF, etc.), enriches them with IBAN/BIC from the supplier record, assembles an ISO 20022 PAIN.001 XML payment batch, and makes it ready for bank submission via PSD2 PISP. FX delta vouchers are written back to Fortnox after execution.

## What it does

1. **Detect** — polls Fortnox `/3/supplierinvoices?filter=unpaid` for FCY invoices
2. **Enrich** — fetches IBAN/BIC from `/3/suppliers/{n}` (one call per unique supplier, cached per request)
3. **Assemble** — generates a `pain.001.001.03` XML batch, grouping payments by currency with SEPA service level for EUR
4. **Download** — exposes the batch as a signed XML download, ready for PISP submission

```
Fortnox API
    │
    ▼
InvoiceSource (domain port)
    │
    ▼
domain.Queue ─── FCY filter ──▶ domain.Enrich ──▶ PAIN.001 XML
                                       │
                               SupplierEnricher (domain port)
                                       │
                               Fortnox Supplier API
```

## Architecture

The core follows hexagonal architecture: domain logic has zero external dependencies, and all I/O is behind explicit ports.

```
internal/
├── domain/          # pure domain — no I/O, no imports from adapter packages
│   ├── invoice.go   # SupplierInvoice, EnrichedInvoice, Queue, Sync, Enrich
│   ├── money.go     # Money (fixed-point int64 minor units, ISO 4217 currency)
│   ├── batch.go     # Batch, BatchItem, BatchStatus lifecycle
│   ├── tenant.go    # TenantID, Tenant, DebtorAccount
│   └── ports.go     # InvoiceSource, SupplierEnricher, BatchRepository,
│                    #   TenantRepository, TokenStore, PaymentSubmitter
│
├── adapter/
│   ├── fortnox/     # Connector — implements InvoiceSource + SupplierEnricher
│   ├── file/        # TokenStore backed by .fortnox-tokens.json (dev/single-process)
│   └── fake/        # Stub implementations for development without Fortnox credentials
│
├── fortnox/         # Raw Fortnox HTTP client, OAuth2, token persistence
├── payment/         # PAIN.001 XML assembly (pain.001.001.03)
├── config/          # Typed env-var configuration
└── ui/              # HTMX + Templ web interface
```

**Key design decisions:**

- `Money` stores amounts as `int64` minor units — no floating-point arithmetic in the domain
- `TokenStore.AtomicRefresh` prevents rolling-token race conditions (CAS semantics)
- Fortnox is an adapter — swapping to Visma or another ERP requires only a new connector
- `TenantID` is first-class, making multi-tenant operation a configuration concern not a rewrite

See [`DECISIONS.md`](DECISIONS.md) for the reasoning behind each architectural choice.

## Prerequisites

- Go 1.26+
- [Task](https://taskfile.dev) (`brew install go-task`)
- A Fortnox sandbox or production account with a registered OAuth2 app

## Configuration

All configuration via environment variables. Copy `.env.example` (or set directly):

| Variable | Required | Description |
|----------|----------|-------------|
| `FORTNOX_CLIENT_ID` | yes | OAuth2 client ID from Fortnox developer portal |
| `FORTNOX_CLIENT_SECRET` | yes | OAuth2 client secret |
| `FORTNOX_REDIRECT_URI` | yes | Callback URI registered with the app |
| `FORTNOX_SCOPES` | no | Space-separated OAuth2 scopes (default: supplier invoice + supplier) |
| `FORTNOX_ENV` | no | `sandbox` (default) or `production` |
| `COBALT_DEBTOR_NAME` | no | Your company name for PAIN.001 debtor block |
| `COBALT_DEBTOR_IBAN` | no | Your IBAN for PAIN.001 debtor block |
| `COBALT_DEBTOR_BIC` | no | Your BIC for PAIN.001 debtor block |
| `PORT` | no | HTTP listen port (default: `8080`) |
| `DATABASE_URL` | no | PostgreSQL connection string (not yet required) |

When `FORTNOX_CLIENT_ID` is absent, the server starts in **dev mode** using hardcoded fake invoices — no credentials needed.

## Quick start (dev mode)

```bash
task dev        # starts server on :8080 with fake invoices
```

Open `http://localhost:8080` — you'll see the invoice list with the batch panel.

## Quick start (Fortnox sandbox)

```bash
# 1. Authenticate once — opens browser for OAuth2 consent
task fortnox:auth

# 2. Run the server
task dev
```

## Development tasks

```bash
task check           # lint + test + vet (run before every commit)
task test            # unit tests only
task test:acceptance # Gherkin acceptance scenarios
task db:migrate      # run pending migrations (requires DATABASE_URL)
task db:migrate:down # roll back last migration
```

## Running in Docker

```bash
docker compose up --build
```

## Migrations

Schema migrations use [golang-migrate](https://github.com/golang-migrate/migrate). Migration files live in `migrations/`.

```bash
DATABASE_URL=postgres://... task db:migrate
```

## Changelog

See [`CHANGELOG.md`](CHANGELOG.md).
