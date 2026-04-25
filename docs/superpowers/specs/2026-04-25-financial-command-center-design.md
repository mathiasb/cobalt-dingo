# Financial Command Center — Design Spec

**Date:** 2026-04-25
**Status:** Draft
**Author:** Mathias + Claude

## Summary

Add a conversational financial analytics layer to cobalt-dingo. A Go MCP server
exposes 38 tools covering six Fortnox ledgers. Primary interface is Claude
(Desktop / Code) via MCP. Secondary interface is an HTMX chat page in the
existing web UI that calls Claude API with the same tool schemas.

## Goals

- Give a small business owner (and their bookkeeper) natural-language access to
  their full financial picture: AP, AR, general ledger, projects, cost centers,
  and assets.
- Produce charts and analyses on demand — markdown tables for simple data,
  Chart.js visualizations for complex data.
- Reuse erp-mafia/fortnox-mcp's tool list as design reference, but implement
  everything in Go within the existing hexagonal architecture.
- Single Fortnox token — no multi-server race conditions.

## Non-goals

- No write operations beyond the existing payment pipeline (no creating invoices,
  customers, or vouchers from the chat).
- No role-based access control for v1 — anyone with the session sees everything.
- No conversation persistence — chat history lives in the browser session.
- No local model routing for v1 — Claude handles all chat queries.

---

## Architecture

### Request flow — MCP (primary)

```
Claude Desktop / Claude Code
  → stdio / SSE
  → cmd/mcp/main.go (MCP server)
  → Tool handler (internal/mcp/tools_*.go)
  → Ledger adapter (internal/adapter/fortnox/*_ledger.go)
  → Fortnox API
  → Structured JSON result
  → Claude formats and presents to user
```

### Request flow — HTMX chat (secondary)

```
Browser
  → POST /chat (user message + conversation history as JSON)
  → ChatHandler (internal/ui/chat.go)
  → Claude API (single call with 38 tool schemas)
  → Claude responds with tool_use blocks
  → ChatHandler executes Go functions locally
  → Claude produces final text response
  → SSE stream back to browser
  → Frontend renders markdown + optional Chart.js charts
```

No "Claude calls Claude" loop. One Claude conversation turn with tool use.
Claude may call multiple tools in a single turn. The Go functions do all
computation (summing, bucketing, sorting). Claude only reasons and explains.

### Component map

| Component | Location | Responsibility |
|-----------|----------|---------------|
| MCP server | `cmd/mcp/main.go` | Entrypoint: create adapters, register tools, start stdio server |
| Tool handlers | `internal/mcp/tools_*.go` | One file per ledger group, dispatch to adapters |
| Aggregator | `internal/analyst/aggregator.go` | Shared grouping, bucketing, aging logic |
| Ledger adapters | `internal/adapter/fortnox/*_ledger.go` | One file per ledger, implement domain ports |
| Shared client | `internal/adapter/fortnox/client.go` | HTTP client, auth, rate limiting, pagination |
| Domain ports | `internal/domain/ports.go` | Extended with 6 ledger interfaces |
| Domain models | `internal/domain/models.go` | New types: CustomerInvoice, Account, Voucher, etc. |
| Chat handler | `internal/ui/chat.go` | HTTP handler, Claude API call, SSE streaming |
| Chat template | `internal/ui/chat.templ` | Chat page with message bubbles, input |
| Layout | `internal/ui/layout.templ` | Updated nav with Chat link |

---

## Domain ports

```go
type SupplierLedger interface {
    UnpaidInvoices(ctx context.Context, tenantID string) ([]SupplierInvoice, error)
    InvoicePayments(ctx context.Context, tenantID string, invoiceNumber int) ([]SupplierPayment, error)
    SupplierDetail(ctx context.Context, tenantID string, supplierNumber int) (Supplier, error)
}

type CustomerLedger interface {
    UnpaidInvoices(ctx context.Context, tenantID string) ([]CustomerInvoice, error)
    InvoicePayments(ctx context.Context, tenantID string, invoiceNumber int) ([]CustomerPayment, error)
    CustomerDetail(ctx context.Context, tenantID string, customerNumber int) (Customer, error)
}

type GeneralLedger interface {
    ChartOfAccounts(ctx context.Context, tenantID string, yearID int) ([]Account, error)
    AccountBalances(ctx context.Context, tenantID string, yearID int, fromAcct, toAcct int) ([]AccountBalance, error)
    AccountActivity(ctx context.Context, tenantID string, yearID int, acctNum int, from, to time.Time) ([]VoucherRow, error)
    Vouchers(ctx context.Context, tenantID string, yearID int, from, to time.Time) ([]Voucher, error)
    VoucherDetail(ctx context.Context, tenantID string, series string, number int) (Voucher, error)
    FinancialYears(ctx context.Context, tenantID string) ([]FinancialYear, error)
    PredefinedAccounts(ctx context.Context, tenantID string) ([]PredefinedAccount, error)
}

type ProjectLedger interface {
    Projects(ctx context.Context, tenantID string) ([]Project, error)
    ProjectTransactions(ctx context.Context, tenantID string, projectID string, from, to time.Time) ([]VoucherRow, error)
}

type CostCenterLedger interface {
    CostCenters(ctx context.Context, tenantID string) ([]CostCenter, error)
    CostCenterTransactions(ctx context.Context, tenantID string, code string, from, to time.Time) ([]VoucherRow, error)
}

type AssetRegister interface {
    Assets(ctx context.Context, tenantID string) ([]Asset, error)
    AssetDetail(ctx context.Context, tenantID string, assetID int) (Asset, error)
}

type CompanyInfo interface {
    Info(ctx context.Context, tenantID string) (Company, error)
}
```

---

## Tool definitions — 38 tools

### Supplier ledger (AP) — 7 tools

| Tool | Args | Returns |
|------|------|---------|
| `ap_summary` | — | Total unpaid count, total by currency, oldest due date |
| `ap_overdue` | `days_threshold?` (default 0) | Overdue invoices with days past due |
| `ap_by_supplier` | `limit?` | Per-supplier totals, sorted by amount desc |
| `ap_by_currency` | — | Per-currency totals |
| `ap_aging` | — | Buckets: 0-30, 31-60, 61-90, 90+ |
| `ap_supplier_history` | `supplier_number` | All invoices + payments for a supplier |
| `ap_invoice_detail` | `invoice_number` | Single invoice with payment history |

### Customer ledger (AR) — 7 tools

| Tool | Args | Returns |
|------|------|---------|
| `ar_summary` | — | Total outstanding count, total by currency |
| `ar_overdue` | `days_threshold?` | Overdue customer invoices |
| `ar_by_customer` | `limit?` | Per-customer totals |
| `ar_aging` | — | Same buckets as AP |
| `ar_customer_history` | `customer_number` | All invoices + payments for a customer |
| `ar_invoice_detail` | `invoice_number` | Single invoice with payment history |
| `ar_unpaid_report` | — | Detailed overdue analysis with contact info |

### General ledger — 7 tools

| Tool | Args | Returns |
|------|------|---------|
| `gl_chart_of_accounts` | `year_id?` | Full account list with descriptions |
| `gl_account_balance` | `account_from, account_to, year_id?` | Balances for account range |
| `gl_account_activity` | `account_num, from_date, to_date, year_id?` | Transactions for account in period |
| `gl_vouchers` | `from_date, to_date, year_id?` | Voucher list in period |
| `gl_voucher_detail` | `series, number` | Full voucher with rows |
| `gl_predefined_accounts` | — | System control accounts |
| `gl_financial_years` | — | Available years with date ranges |

### Project ledger — 3 tools

| Tool | Args | Returns |
|------|------|---------|
| `project_list` | — | All projects with status |
| `project_transactions` | `project_id, from_date?, to_date?` | Voucher rows tagged to project |
| `project_profitability` | `project_id` | Revenue/cost/margin summary |

### Cost center ledger — 3 tools

| Tool | Args | Returns |
|------|------|---------|
| `costcenter_list` | — | All cost centers |
| `costcenter_transactions` | `code, from_date?, to_date?` | Voucher rows tagged to cost center |
| `costcenter_analysis` | `code` | Cost breakdown by account category |

### Asset register — 2 tools

| Tool | Args | Returns |
|------|------|---------|
| `asset_list` | — | All assets with book value, depreciation method |
| `asset_detail` | `asset_id` | Full asset with depreciation schedule |

### Analytics / BI — 9 tools

| Tool | Args | Returns |
|------|------|---------|
| `cash_flow_forecast` | `days_ahead?` (default 30) | Projected in/outflows from open AR + AP |
| `expense_analysis` | `from_date, to_date` | Costs grouped by account category |
| `period_comparison` | `period1_from, period1_to, period2_from, period2_to` | Revenue, cost, margin side by side |
| `yearly_comparison` | `year1, year2` | Year-over-year on key metrics |
| `gross_margin_trend` | `months?` (default 12) | Monthly margin evolution |
| `top_customers` | `from_date, to_date, limit?` | Highest-revenue customers |
| `top_suppliers` | `from_date, to_date, limit?` | Highest-cost suppliers |
| `sales_vs_purchases` | `from_date, to_date, granularity?` | Revenue vs. cost trend |
| `company_info` | — | Company profile, org number, fiscal year |

---

## Fortnox adapter layer

### Shared client extensions

- **Rate limiter:** `rate.Limiter` at 25 req/5 sec, shared across all adapters
- **Pagination:** transparent `?page=N` collection for list endpoints
- **Error mapping:** Fortnox error codes → domain errors

### Caching strategy

| Data | Strategy | Rationale |
|------|----------|-----------|
| Chart of accounts | Cache per financial year | Rarely changes |
| Financial years | Cache per session | Never changes mid-session |
| Predefined accounts | Cache per financial year | Rarely changes |
| Supplier/customer detail | 5-min TTL (existing pattern) | Changes infrequently |
| Invoice/voucher data | Always live | This is what users are asking about |
| Company info | Cache per session | Never changes mid-session |

### Fortnox endpoints used

| Adapter | Endpoints |
|---------|-----------|
| Supplier ledger | `GET /3/supplierinvoices`, `GET /3/supplierinvoicepayments`, `GET /3/suppliers/{n}` |
| Customer ledger | `GET /3/invoices`, `GET /3/invoicepayments`, `GET /3/customers/{n}` |
| General ledger | `GET /3/accounts`, `GET /3/vouchers`, `GET /3/vouchers/{s}/{n}`, `GET /3/financialyears`, `GET /3/predefinedaccounts` |
| Project ledger | `GET /3/projects`, vouchers filtered by project |
| Cost center ledger | `GET /3/costcenters`, vouchers filtered by cost center |
| Asset register | `GET /3/assets`, `GET /3/assets/{id}` |
| Company | `GET /3/companyinformation` |

---

## MCP server

### Transport

- `stdio` for Claude Code
- `SSE` for Claude Desktop and programmatic access

### Library

`mark3labs/mcp-go` — standard Go MCP SDK.

### Entrypoint

`cmd/mcp/main.go` — creates Fortnox client with env-var config, instantiates
all ledger adapters, registers 38 tools with JSON schemas, starts server.

### Claude configuration

```json
{
  "mcpServers": {
    "cobalt-dingo": {
      "command": "go",
      "args": ["run", "./cmd/mcp"],
      "env": {
        "FORTNOX_CLIENT_ID": "...",
        "FORTNOX_CLIENT_SECRET": "...",
        "FORTNOX_TOKENS_FILE": "~/.fortnox-tokens.json"
      }
    }
  }
}
```

---

## HTMX chat (secondary UI)

### Route

`GET /chat` — serves chat page. `POST /chat` — handles message, streams response via SSE.

### Implementation

- ChatHandler sends user message + conversation history to Claude API with 38
  tool schemas
- Claude responds with `tool_use` blocks; handler executes Go functions locally
- Claude produces final text response; streamed back via SSE
- No "Claude calls Claude" — one conversation turn with tool use
- Conversation history sent as JSON in request body, no server-side session

### Rendering

- Markdown → HTML via goldmark, rendered in Templ components
- Charts: Claude includes `<div data-chart='{"type":"bar",...}'/>` in response;
  ~50 lines of JS renders with Chart.js (CDN, no build step)
- Tables: standard markdown tables rendered by goldmark

### UI components

- `ChatPage` — full page with message thread and input
- `MessageBubble` — user or assistant message
- `ChatInput` — text input with send button
- Navigation updated with Chat link alongside existing AP automation

---

## Project structure — new and modified files

```
cmd/
├── server/main.go              # Modified: add /chat route, wire new adapters
└── mcp/main.go                 # New: MCP server entrypoint

internal/
├── mcp/
│   ├── server.go               # New: MCP protocol, tool registration
│   ├── tools_ap.go             # New: 7 AP tool handlers
│   ├── tools_ar.go             # New: 7 AR tool handlers
│   ├── tools_gl.go             # New: 7 GL tool handlers
│   ├── tools_project.go        # New: 3 project tool handlers
│   ├── tools_costcenter.go     # New: 3 cost center tool handlers
│   ├── tools_asset.go          # New: 2 asset tool handlers
│   └── tools_analytics.go      # New: 9 analytics tool handlers
├── analyst/
│   └── aggregator.go           # New: shared grouping, bucketing, aging
├── adapter/fortnox/
│   ├── client.go               # Modified: rate limiter, pagination
│   ├── supplier_ledger.go      # New (refactored from existing client.go + supplier.go)
│   ├── customer_ledger.go      # New
│   ├── general_ledger.go       # New
│   ├── project_ledger.go       # New
│   ├── costcenter_ledger.go    # New
│   ├── asset_register.go       # New
│   ├── company.go              # New
│   ├── erp_writer.go           # Existing, unchanged
│   ├── supplier_cache.go       # Existing, unchanged
│   └── auth.go                 # Existing, unchanged
├── domain/
│   ├── ports.go                # Modified: add 6 ledger + company interfaces
│   ├── models.go               # New: CustomerInvoice, Account, Voucher, etc.
│   └── ...                     # Existing, unchanged
└── ui/
    ├── chat.templ               # New
    ├── chat.go                  # New
    ├── layout.templ             # Modified: add Chat nav link
    └── ...                      # Existing, unchanged
```

---

## Design reference

erp-mafia/fortnox-mcp (TypeScript, 45+ tools) used as design reference for tool
definitions and analytics patterns. No code or runtime dependency taken.
Repository: https://github.com/erp-mafia/fortnox-mcp

---

## Open questions

- **Account balance endpoint:** Verify `GET /3/balancesaccounting` exists and
  returns period balances, or if we need to derive from voucher rows.
- **Asset API:** Confirm `GET /3/assets` is available on the Fortnox plan
  typically used by SMEs (may require a specific module).
- **Chart.js bundle size:** Evaluate whether a lighter alternative (uPlot,
  Frappe Charts) is worth it for the few chart types we need.
