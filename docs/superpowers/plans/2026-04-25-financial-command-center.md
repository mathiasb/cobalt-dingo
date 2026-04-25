# Financial Command Center Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a Go MCP server exposing 38 tools across six Fortnox ledgers, usable from Claude Desktop/Code and an HTMX chat fallback UI.

**Architecture:** Hexagonal — new domain port interfaces per ledger, Fortnox HTTP adapters implementing them, MCP tool handlers dispatching to adapters, shared aggregation logic. Two entrypoints: `cmd/mcp` (stdio MCP server) and `cmd/server` (existing web server + new `/chat` route).

**Tech Stack:** Go, mcp-go (mark3labs/mcp-go), Fortnox REST API v3, HTMX + Templ + SSE, Claude API (tool use), Chart.js, goldmark (markdown rendering).

**Spec:** `docs/superpowers/specs/2026-04-25-financial-command-center-design.md`

---

## File Structure

### New files

```
internal/domain/
├── ledger.go                    # New domain types: CustomerInvoice, Account, Voucher, etc.
├── ledger_ports.go              # 7 new port interfaces (SupplierLedger, CustomerLedger, etc.)

internal/analyst/
├── aggregator.go                # Shared grouping, bucketing, aging functions
├── aggregator_test.go           # Table-driven tests for aggregation logic

internal/adapter/fortnox/
├── supplier_ledger.go           # SupplierLedger port implementation
├── supplier_ledger_test.go      # Tests with HTTP test server
├── customer_ledger.go           # CustomerLedger port implementation
├── customer_ledger_test.go
├── general_ledger.go            # GeneralLedger port implementation
├── general_ledger_test.go
├── project_ledger.go            # ProjectLedger port implementation
├── project_ledger_test.go
├── costcenter_ledger.go         # CostCenterLedger port implementation
├── costcenter_ledger_test.go
├── asset_register.go            # AssetRegister port implementation
├── asset_register_test.go
├── company.go                   # CompanyInfo port implementation
├── company_test.go

internal/mcp/
├── server.go                    # MCP server setup, tool registration
├── tools_ap.go                  # 7 AP tool handlers
├── tools_ar.go                  # 7 AR tool handlers
├── tools_gl.go                  # 7 GL tool handlers
├── tools_project.go             # 3 project tool handlers
├── tools_costcenter.go          # 3 cost center tool handlers
├── tools_asset.go               # 2 asset tool handlers
├── tools_analytics.go           # 9 analytics tool handlers
├── tools_test.go                # Integration tests for tool handlers

cmd/mcp/
├── main.go                      # MCP server entrypoint

internal/ui/
├── chat.templ                   # Chat page template
├── chat.go                      # Chat HTTP handler with Claude API + SSE
```

### Modified files

```
internal/adapter/fortnox/client.go   # Extend: rate limiter, pagination helper
  → Currently only exists as internal/fortnox/client.go (low-level)
  → connector.go is the adapter layer — extend with shared helpers

internal/fortnox/client.go           # Add: generic GET helper, pagination, new endpoint methods
internal/ui/layout.templ             # Add: Chat nav link
internal/config/config.go            # Add: Claude API config, llama-swap config
cmd/server/main.go                   # Wire new adapters, add /chat route
go.mod                               # Add: mark3labs/mcp-go, goldmark, anthropic SDK
```

---

## Phase 1: Domain Layer + Shared Infrastructure

### Task 1: Domain types for ledger data

**Files:**
- Create: `internal/domain/ledger.go`

- [ ] **Step 1: Create domain types file**

```go
// internal/domain/ledger.go
package domain

import "time"

// CustomerInvoice is the ERP-agnostic domain model for a customer invoice.
type CustomerInvoice struct {
	InvoiceNumber  int
	CustomerNumber int
	CustomerName   string
	Amount         Money
	Balance        Money // outstanding balance (may differ from Amount if partially paid)
	DueDate        string
	InvoiceDate    string
	Booked         bool
	Cancelled      bool
	Sent           bool
}

// SupplierPayment records a payment made against a supplier invoice.
type SupplierPayment struct {
	PaymentNumber int
	InvoiceNumber int
	Amount        Money
	CurrencyRate  float64
	PaymentDate   string
	Booked        bool
}

// CustomerPayment records a payment received against a customer invoice.
type CustomerPayment struct {
	PaymentNumber int
	InvoiceNumber int
	Amount        Money
	PaymentDate   string
	Booked        bool
}

// Supplier holds supplier master data relevant for AP analysis.
type Supplier struct {
	SupplierNumber int
	Name           string
	Email          string
	Phone          string
	IBAN           string
	BIC            string
	Active         bool
}

// Customer holds customer master data relevant for AR analysis.
type Customer struct {
	CustomerNumber int
	Name           string
	Email          string
	Phone          string
	Active         bool
}

// Account is a general ledger account from the chart of accounts.
type Account struct {
	Number      int
	Description string
	SRU         int    // Swedish standard tax reporting code
	Active      bool
	Year        int    // financial year ID this belongs to
	BalanceBF   Money  // balance brought forward
	BalanceCF   Money  // balance carried forward
}

// AccountBalance holds period balances for a single account.
type AccountBalance struct {
	AccountNumber int
	Period        string // YYYY-MM or full year
	Balance       Money
}

// VoucherRow is a single line in a voucher (journal entry).
type VoucherRow struct {
	Account     int
	Debit       Money
	Credit      Money
	Description string
	CostCenter  string
	Project     string
}

// Voucher is a complete journal entry with header and rows.
type Voucher struct {
	Series         string
	Number         int
	Description    string
	TransactionDate string
	Year           int
	Rows           []VoucherRow
}

// FinancialYear defines a fiscal year's boundaries.
type FinancialYear struct {
	ID   int
	From time.Time
	To   time.Time
}

// PredefinedAccount maps a system role to an account number.
type PredefinedAccount struct {
	Name    string // e.g. "AP", "AR", "BANK"
	Account int
}

// Project represents an active project for cost/revenue tracking.
type Project struct {
	Number      string
	Description string
	Status      string // "ONGOING", "COMPLETED", etc.
	StartDate   string
	EndDate     string
}

// CostCenter represents a cost center for departmental tracking.
type CostCenter struct {
	Code        string
	Description string
	Active      bool
}

// Asset represents a fixed asset from the asset register.
type Asset struct {
	ID                  int
	Number              string
	Description         string
	AcquisitionDate     string
	AcquisitionValue    Money
	DepreciationMethod  string // "STRAIGHT_LINE", "DECLINING_BALANCE"
	DepreciationPercent float64
	BookValue           Money
	AccumulatedDepr     Money
}

// Company holds basic company profile information.
type Company struct {
	Name           string
	OrgNumber      string
	Address        string
	City           string
	ZipCode        string
	Country        string
	Email          string
	Phone          string
	VisitAddress   string
	VisitCity      string
	VisitZipCode   string
}
```

- [ ] **Step 2: Verify it compiles**

Run: `cd /Users/mathias/Documents/local-dev/AGENTS/cobalt-dingo && go build ./internal/domain/...`
Expected: no errors

- [ ] **Step 3: Commit**

```bash
git add internal/domain/ledger.go
git commit -m "feat: add domain types for ledger data"
```

---

### Task 2: Ledger port interfaces

**Files:**
- Create: `internal/domain/ledger_ports.go`

- [ ] **Step 1: Create port interfaces**

```go
// internal/domain/ledger_ports.go
package domain

import (
	"context"
	"time"
)

// SupplierLedger provides access to the accounts payable sub-ledger.
type SupplierLedger interface {
	UnpaidInvoices(ctx context.Context, tenantID TenantID) ([]SupplierInvoice, error)
	InvoicePayments(ctx context.Context, tenantID TenantID, invoiceNumber int) ([]SupplierPayment, error)
	SupplierDetail(ctx context.Context, tenantID TenantID, supplierNumber int) (Supplier, error)
}

// CustomerLedger provides access to the accounts receivable sub-ledger.
type CustomerLedger interface {
	UnpaidInvoices(ctx context.Context, tenantID TenantID) ([]CustomerInvoice, error)
	InvoicePayments(ctx context.Context, tenantID TenantID, invoiceNumber int) ([]CustomerPayment, error)
	CustomerDetail(ctx context.Context, tenantID TenantID, customerNumber int) (Customer, error)
}

// GeneralLedger provides access to the general ledger and chart of accounts.
type GeneralLedger interface {
	ChartOfAccounts(ctx context.Context, tenantID TenantID, yearID int) ([]Account, error)
	AccountBalances(ctx context.Context, tenantID TenantID, yearID int, fromAcct, toAcct int) ([]AccountBalance, error)
	AccountActivity(ctx context.Context, tenantID TenantID, yearID int, acctNum int, from, to time.Time) ([]VoucherRow, error)
	Vouchers(ctx context.Context, tenantID TenantID, yearID int, from, to time.Time) ([]Voucher, error)
	VoucherDetail(ctx context.Context, tenantID TenantID, series string, number int) (Voucher, error)
	FinancialYears(ctx context.Context, tenantID TenantID) ([]FinancialYear, error)
	PredefinedAccounts(ctx context.Context, tenantID TenantID) ([]PredefinedAccount, error)
}

// ProjectLedger provides access to project-based cost and revenue tracking.
type ProjectLedger interface {
	Projects(ctx context.Context, tenantID TenantID) ([]Project, error)
	ProjectTransactions(ctx context.Context, tenantID TenantID, projectID string, from, to time.Time) ([]VoucherRow, error)
}

// CostCenterLedger provides access to cost center-based tracking.
type CostCenterLedger interface {
	CostCenters(ctx context.Context, tenantID TenantID) ([]CostCenter, error)
	CostCenterTransactions(ctx context.Context, tenantID TenantID, code string, from, to time.Time) ([]VoucherRow, error)
}

// AssetRegister provides access to the fixed asset register.
type AssetRegister interface {
	Assets(ctx context.Context, tenantID TenantID) ([]Asset, error)
	AssetDetail(ctx context.Context, tenantID TenantID, assetID int) (Asset, error)
}

// CompanyInfo provides access to company profile data.
type CompanyInfo interface {
	Info(ctx context.Context, tenantID TenantID) (Company, error)
}
```

- [ ] **Step 2: Verify it compiles**

Run: `cd /Users/mathias/Documents/local-dev/AGENTS/cobalt-dingo && go build ./internal/domain/...`
Expected: no errors

- [ ] **Step 3: Commit**

```bash
git add internal/domain/ledger_ports.go
git commit -m "feat: add ledger port interfaces"
```

---

### Task 3: Aggregation library

**Files:**
- Create: `internal/analyst/aggregator_test.go`
- Create: `internal/analyst/aggregator.go`

- [ ] **Step 1: Write failing tests for aging buckets**

```go
// internal/analyst/aggregator_test.go
package analyst_test

import (
	"testing"
	"time"

	"github.com/mathiasb/cobalt-dingo/internal/analyst"
)

func TestAgingBuckets(t *testing.T) {
	today := time.Date(2026, 4, 25, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name     string
		dueDates []string
		want     analyst.AgingReport
	}{
		{
			name:     "single current invoice",
			dueDates: []string{"2026-05-01"},
			want:     analyst.AgingReport{Current: 1},
		},
		{
			name:     "mixed buckets",
			dueDates: []string{"2026-04-20", "2026-03-01", "2026-02-01", "2026-01-01"},
			want:     analyst.AgingReport{Days1To30: 1, Days31To60: 1, Days61To90: 1, Over90: 1},
		},
		{
			name:     "empty input",
			dueDates: []string{},
			want:     analyst.AgingReport{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := analyst.AgingBuckets(tt.dueDates, today)
			if got != tt.want {
				t.Errorf("AgingBuckets() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestGroupByString(t *testing.T) {
	type item struct {
		Key   string
		Value int
	}

	items := []item{
		{"EUR", 100},
		{"USD", 200},
		{"EUR", 300},
	}

	got := analyst.GroupBy(items, func(i item) string { return i.Key })

	if len(got) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(got))
	}
	if len(got["EUR"]) != 2 {
		t.Errorf("expected 2 EUR items, got %d", len(got["EUR"]))
	}
	if len(got["USD"]) != 1 {
		t.Errorf("expected 1 USD item, got %d", len(got["USD"]))
	}
}

func TestSumMinorUnits(t *testing.T) {
	values := []int64{10050, 20075, 5025}
	got := analyst.SumMinorUnits(values)
	want := int64(35150)
	if got != want {
		t.Errorf("SumMinorUnits() = %d, want %d", got, want)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /Users/mathias/Documents/local-dev/AGENTS/cobalt-dingo && go test ./internal/analyst/... -v`
Expected: compilation errors (package doesn't exist yet)

- [ ] **Step 3: Implement aggregation functions**

```go
// internal/analyst/aggregator.go
package analyst

import "time"

// AgingReport counts items by overdue age bucket.
type AgingReport struct {
	Current   int `json:"current"`
	Days1To30 int `json:"days_1_to_30"`
	Days31To60 int `json:"days_31_to_60"`
	Days61To90 int `json:"days_61_to_90"`
	Over90    int `json:"over_90"`
}

// AgingBuckets classifies due dates into aging buckets relative to today.
func AgingBuckets(dueDates []string, today time.Time) AgingReport {
	var r AgingReport
	for _, ds := range dueDates {
		d, err := time.Parse("2006-01-02", ds)
		if err != nil {
			continue
		}
		days := int(today.Sub(d).Hours() / 24)
		switch {
		case days <= 0:
			r.Current++
		case days <= 30:
			r.Days1To30++
		case days <= 60:
			r.Days31To60++
		case days <= 90:
			r.Days61To90++
		default:
			r.Over90++
		}
	}
	return r
}

// GroupBy groups a slice by a key function.
func GroupBy[T any, K comparable](items []T, keyFn func(T) K) map[K][]T {
	result := make(map[K][]T)
	for _, item := range items {
		k := keyFn(item)
		result[k] = append(result[k], item)
	}
	return result
}

// SumMinorUnits sums a slice of int64 values (typically Money.MinorUnits).
func SumMinorUnits(values []int64) int64 {
	var total int64
	for _, v := range values {
		total += v
	}
	return total
}

// DaysOverdue returns the number of days past due, or 0 if not yet due.
func DaysOverdue(dueDate string, today time.Time) int {
	d, err := time.Parse("2006-01-02", dueDate)
	if err != nil {
		return 0
	}
	days := int(today.Sub(d).Hours() / 24)
	if days < 0 {
		return 0
	}
	return days
}

// OrderedKeys returns map keys in the order they first appeared in the input.
func OrderedKeys[T any, K comparable](items []T, keyFn func(T) K) []K {
	seen := make(map[K]bool)
	var keys []K
	for _, item := range items {
		k := keyFn(item)
		if !seen[k] {
			seen[k] = true
			keys = append(keys, k)
		}
	}
	return keys
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/mathias/Documents/local-dev/AGENTS/cobalt-dingo && go test ./internal/analyst/... -v`
Expected: all 3 tests PASS

- [ ] **Step 5: Commit**

```bash
git add internal/analyst/aggregator.go internal/analyst/aggregator_test.go
git commit -m "feat: add aggregation library for aging, grouping, summing"
```

---

### Task 4: Extend Fortnox HTTP client with pagination and rate limiting

**Files:**
- Modify: `internal/fortnox/client.go`
- Create: `internal/fortnox/client_test.go`

- [ ] **Step 1: Write failing test for pagination**

```go
// internal/fortnox/client_test.go
package fortnox_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mathiasb/cobalt-dingo/internal/fortnox"
)

func TestGetAllPages(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		page := r.URL.Query().Get("page")
		var resp map[string]any
		switch page {
		case "", "1":
			resp = map[string]any{
				"MetaInformation": map[string]any{
					"@TotalPages":   2,
					"@CurrentPage":  1,
					"@TotalResources": 3,
				},
				"Items": []map[string]any{
					{"ID": 1},
					{"ID": 2},
				},
			}
		case "2":
			resp = map[string]any{
				"MetaInformation": map[string]any{
					"@TotalPages":   2,
					"@CurrentPage":  2,
					"@TotalResources": 3,
				},
				"Items": []map[string]any{
					{"ID": 3},
				},
			}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	client := fortnox.NewClient(srv.URL, "test-token")
	pages, err := client.GetAllPages(srv.URL + "/test")
	if err != nil {
		t.Fatalf("GetAllPages() error: %v", err)
	}
	if len(pages) != 2 {
		t.Fatalf("expected 2 pages, got %d", len(pages))
	}
	if callCount != 2 {
		t.Errorf("expected 2 HTTP calls, got %d", callCount)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/mathias/Documents/local-dev/AGENTS/cobalt-dingo && go test ./internal/fortnox/... -run TestGetAllPages -v`
Expected: FAIL — `GetAllPages` not defined

- [ ] **Step 3: Add pagination and rate-limited GET to client**

Add the following to `internal/fortnox/client.go`:

```go
import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/mathiasb/cobalt-dingo/internal/domain"
)

// rateLimiter enforces Fortnox's 25 req/5 sec limit across all clients.
var (
	rateMu     sync.Mutex
	rateTokens = 25
	rateReset  = time.Now()
)

func waitForRate() {
	rateMu.Lock()
	defer rateMu.Unlock()
	now := time.Now()
	if now.Sub(rateReset) >= 5*time.Second {
		rateTokens = 25
		rateReset = now
	}
	if rateTokens <= 0 {
		wait := 5*time.Second - now.Sub(rateReset)
		rateMu.Unlock()
		time.Sleep(wait)
		rateMu.Lock()
		rateTokens = 25
		rateReset = time.Now()
	}
	rateTokens--
}

// MetaInformation is the pagination metadata returned by Fortnox list endpoints.
type MetaInformation struct {
	TotalPages     int `json:"@TotalPages"`
	CurrentPage    int `json:"@CurrentPage"`
	TotalResources int `json:"@TotalResources"`
}

// PageResponse is a raw Fortnox response with pagination metadata.
type PageResponse struct {
	Meta    MetaInformation
	RawJSON json.RawMessage
}

// Get performs an authenticated GET request and returns the raw JSON body.
func (c *Client) Get(requestURL string) (json.RawMessage, error) {
	waitForRate()
	req, err := http.NewRequest(http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", requestURL, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: status %d", requestURL, resp.StatusCode)
	}

	var body json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return body, nil
}

// GetAllPages fetches all pages from a paginated Fortnox endpoint.
// Returns one raw JSON body per page.
func (c *Client) GetAllPages(baseURL string) ([]json.RawMessage, error) {
	var pages []json.RawMessage

	u, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("parse URL: %w", err)
	}

	for page := 1; ; page++ {
		q := u.Query()
		q.Set("page", fmt.Sprintf("%d", page))
		u.RawQuery = q.Encode()

		body, err := c.Get(u.String())
		if err != nil {
			return nil, fmt.Errorf("page %d: %w", page, err)
		}

		pages = append(pages, body)

		// Extract MetaInformation to check if there are more pages.
		var envelope struct {
			Meta MetaInformation `json:"MetaInformation"`
		}
		if err := json.Unmarshal(body, &envelope); err != nil {
			// No MetaInformation — assume single page.
			break
		}
		if envelope.Meta.TotalPages == 0 || page >= envelope.Meta.TotalPages {
			break
		}
	}

	return pages, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /Users/mathias/Documents/local-dev/AGENTS/cobalt-dingo && go test ./internal/fortnox/... -run TestGetAllPages -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/fortnox/client.go internal/fortnox/client_test.go
git commit -m "feat: add paginated GET and rate limiting to Fortnox client"
```

---

## Phase 2: Fortnox Ledger Adapters

### Task 5: Supplier ledger adapter

**Files:**
- Create: `internal/adapter/fortnox/supplier_ledger.go`
- Create: `internal/adapter/fortnox/supplier_ledger_test.go`
- Modify: `internal/fortnox/client.go` — add `ListSupplierInvoicePayments` and `GetSupplier` methods

- [ ] **Step 1: Add raw client methods for supplier invoice payments and full supplier detail**

Add to `internal/fortnox/client.go`:

```go
// SupplierInvoicePaymentRow is the Fortnox JSON for a supplier invoice payment.
type SupplierInvoicePaymentRow struct {
	Number        int     `json:"Number"`
	InvoiceNumber int     `json:"InvoiceNumber"`
	Amount        float64 `json:"Amount"`
	AmountCurrency float64 `json:"AmountCurrency"`
	Currency      string  `json:"Currency"`
	CurrencyRate  float64 `json:"CurrencyRate"`
	PaymentDate   string  `json:"PaymentDate"`
	Booked        bool    `json:"Booked"`
}

// SupplierInvoicePaymentsResponse wraps the Fortnox payments list.
type SupplierInvoicePaymentsResponse struct {
	SupplierInvoicePayments []SupplierInvoicePaymentRow `json:"SupplierInvoicePayments"`
}

// ListSupplierInvoicePayments fetches payments for a specific supplier invoice.
func (c *Client) ListSupplierInvoicePayments(invoiceNumber int) ([]SupplierInvoicePaymentRow, error) {
	url := fmt.Sprintf("%s/3/supplierinvoicepayments?filter=invoicenumber&invoicenumber=%d", c.baseURL, invoiceNumber)
	body, err := c.Get(url)
	if err != nil {
		return nil, fmt.Errorf("list supplier invoice payments: %w", err)
	}
	var envelope SupplierInvoicePaymentsResponse
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("decode supplier invoice payments: %w", err)
	}
	return envelope.SupplierInvoicePayments, nil
}

// FullSupplierRow has all supplier fields relevant for analysis.
type FullSupplierRow struct {
	SupplierNumber int    `json:"SupplierNumber"`
	Name           string `json:"Name"`
	Email          string `json:"Email"`
	Phone          string `json:"Phone1"`
	IBAN           string `json:"IBAN"`
	BIC            string `json:"BIC"`
	Active         bool   `json:"Active"`
}

// FullSupplierResponse wraps a full supplier detail response.
type FullSupplierResponse struct {
	Supplier FullSupplierRow `json:"Supplier"`
}

// GetFullSupplier fetches full supplier details.
func (c *Client) GetFullSupplier(supplierNumber int) (FullSupplierRow, error) {
	url := fmt.Sprintf("%s/3/suppliers/%d", c.baseURL, supplierNumber)
	body, err := c.Get(url)
	if err != nil {
		return FullSupplierRow{}, fmt.Errorf("get supplier %d: %w", supplierNumber, err)
	}
	var envelope FullSupplierResponse
	if err := json.Unmarshal(body, &envelope); err != nil {
		return FullSupplierRow{}, fmt.Errorf("decode supplier %d: %w", supplierNumber, err)
	}
	return envelope.Supplier, nil
}
```

- [ ] **Step 2: Write failing test for SupplierLedgerAdapter**

```go
// internal/adapter/fortnox/supplier_ledger_test.go
package fortnox_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	adapterfortnox "github.com/mathiasb/cobalt-dingo/internal/adapter/fortnox"
	"github.com/mathiasb/cobalt-dingo/internal/domain"
)

// stubTokenStore returns a fixed token for any tenant.
type stubTokenStore struct{}

func (s stubTokenStore) Load(_ context.Context, _ domain.TenantID) (domain.OAuthToken, error) {
	return domain.OAuthToken{AccessToken: "test", ExpiresAt: fixedFuture}, nil
}
func (s stubTokenStore) AtomicRefresh(_ context.Context, _ domain.TenantID, _, _ domain.OAuthToken) error {
	return nil
}

var fixedFuture = time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC)

func TestSupplierLedger_UnpaidInvoices(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"SupplierInvoices": []map[string]any{
				{
					"InvoiceNumber":        1001,
					"SupplierNumber":       42,
					"SupplierName":         "Acme GmbH",
					"Currency":             "EUR",
					"TotalInvoiceCurrency": 1500.0,
					"DueDate":              "2026-04-20",
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	adapter := adapterfortnox.NewSupplierLedgerAdapter(srv.URL, stubTokenStore{})
	invoices, err := adapter.UnpaidInvoices(context.Background(), "test-tenant")
	if err != nil {
		t.Fatalf("UnpaidInvoices() error: %v", err)
	}
	if len(invoices) != 1 {
		t.Fatalf("expected 1 invoice, got %d", len(invoices))
	}
	if invoices[0].InvoiceNumber != 1001 {
		t.Errorf("expected invoice 1001, got %d", invoices[0].InvoiceNumber)
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `cd /Users/mathias/Documents/local-dev/AGENTS/cobalt-dingo && go test ./internal/adapter/fortnox/... -run TestSupplierLedger -v`
Expected: FAIL — `NewSupplierLedgerAdapter` not defined

- [ ] **Step 4: Implement SupplierLedgerAdapter**

```go
// internal/adapter/fortnox/supplier_ledger.go
package fortnox

import (
	"context"
	"fmt"

	"github.com/mathiasb/cobalt-dingo/internal/domain"
	rawfortnox "github.com/mathiasb/cobalt-dingo/internal/fortnox"
)

// SupplierLedgerAdapter implements domain.SupplierLedger using the Fortnox API.
type SupplierLedgerAdapter struct {
	baseURL string
	tokens  domain.TokenStore
}

// NewSupplierLedgerAdapter creates a new SupplierLedgerAdapter.
func NewSupplierLedgerAdapter(baseURL string, tokens domain.TokenStore) *SupplierLedgerAdapter {
	return &SupplierLedgerAdapter{baseURL: baseURL, tokens: tokens}
}

func (a *SupplierLedgerAdapter) client(ctx context.Context, tenantID domain.TenantID) (*rawfortnox.Client, error) {
	tok, err := a.tokens.Load(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("load token: %w", err)
	}
	return rawfortnox.NewClient(a.baseURL, tok.AccessToken), nil
}

// UnpaidInvoices implements domain.SupplierLedger.
func (a *SupplierLedgerAdapter) UnpaidInvoices(ctx context.Context, tenantID domain.TenantID) ([]domain.SupplierInvoice, error) {
	c, err := a.client(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("supplier ledger: %w", err)
	}
	return c.UnpaidSupplierInvoices()
}

// InvoicePayments implements domain.SupplierLedger.
func (a *SupplierLedgerAdapter) InvoicePayments(ctx context.Context, tenantID domain.TenantID, invoiceNumber int) ([]domain.SupplierPayment, error) {
	c, err := a.client(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("supplier ledger: %w", err)
	}
	rows, err := c.ListSupplierInvoicePayments(invoiceNumber)
	if err != nil {
		return nil, fmt.Errorf("supplier invoice payments: %w", err)
	}
	payments := make([]domain.SupplierPayment, len(rows))
	for i, r := range rows {
		payments[i] = domain.SupplierPayment{
			PaymentNumber: r.Number,
			InvoiceNumber: r.InvoiceNumber,
			Amount:        domain.MoneyFromFloat(r.AmountCurrency, r.Currency),
			CurrencyRate:  r.CurrencyRate,
			PaymentDate:   r.PaymentDate,
			Booked:        r.Booked,
		}
	}
	return payments, nil
}

// SupplierDetail implements domain.SupplierLedger.
func (a *SupplierLedgerAdapter) SupplierDetail(ctx context.Context, tenantID domain.TenantID, supplierNumber int) (domain.Supplier, error) {
	c, err := a.client(ctx, tenantID)
	if err != nil {
		return domain.Supplier{}, fmt.Errorf("supplier ledger: %w", err)
	}
	row, err := c.GetFullSupplier(supplierNumber)
	if err != nil {
		return domain.Supplier{}, fmt.Errorf("supplier detail: %w", err)
	}
	return domain.Supplier{
		SupplierNumber: row.SupplierNumber,
		Name:           row.Name,
		Email:          row.Email,
		Phone:          row.Phone,
		IBAN:           row.IBAN,
		BIC:            row.BIC,
		Active:         row.Active,
	}, nil
}
```

- [ ] **Step 5: Fix test import (add `time` package) and run tests**

Run: `cd /Users/mathias/Documents/local-dev/AGENTS/cobalt-dingo && go test ./internal/adapter/fortnox/... -run TestSupplierLedger -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/fortnox/client.go internal/adapter/fortnox/supplier_ledger.go internal/adapter/fortnox/supplier_ledger_test.go
git commit -m "feat: add supplier ledger adapter"
```

---

### Task 6: Customer ledger adapter

**Files:**
- Create: `internal/fortnox/customer.go` — raw client methods for customer invoices and payments
- Create: `internal/adapter/fortnox/customer_ledger.go`
- Create: `internal/adapter/fortnox/customer_ledger_test.go`

- [ ] **Step 1: Add raw client methods for customer invoices, payments, and detail**

```go
// internal/fortnox/customer.go
package fortnox

import (
	"encoding/json"
	"fmt"

	"github.com/mathiasb/cobalt-dingo/internal/domain"
)

// CustomerInvoiceRow is the Fortnox JSON representation of a customer invoice.
type CustomerInvoiceRow struct {
	DocumentNumber int     `json:"DocumentNumber"`
	CustomerNumber string  `json:"CustomerNumber"`
	CustomerName   string  `json:"CustomerName"`
	Currency       string  `json:"Currency"`
	Total          float64 `json:"Total"`
	Balance        float64 `json:"Balance"`
	DueDate        string  `json:"DueDate"`
	InvoiceDate    string  `json:"InvoiceDate"`
	Booked         bool    `json:"Booked"`
	Cancelled      bool    `json:"Cancelled"`
	Sent           bool    `json:"Sent"`
}

// CustomerInvoicesResponse wraps the Fortnox customer invoices list.
type CustomerInvoicesResponse struct {
	Invoices []CustomerInvoiceRow `json:"Invoices"`
}

// UnpaidCustomerInvoices fetches all unpaid customer invoices from Fortnox.
func (c *Client) UnpaidCustomerInvoices() ([]CustomerInvoiceRow, error) {
	body, err := c.Get(c.baseURL + "/3/invoices?filter=unpaid")
	if err != nil {
		return nil, fmt.Errorf("list unpaid customer invoices: %w", err)
	}
	var envelope CustomerInvoicesResponse
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("decode customer invoices: %w", err)
	}
	return envelope.Invoices, nil
}

// CustomerPaymentRow is the Fortnox JSON for a customer invoice payment.
type CustomerPaymentRow struct {
	Number        int     `json:"Number"`
	InvoiceNumber int     `json:"InvoiceNumber"`
	Amount        float64 `json:"Amount"`
	AmountCurrency float64 `json:"AmountCurrency"`
	Currency      string  `json:"Currency"`
	PaymentDate   string  `json:"PaymentDate"`
	Booked        bool    `json:"Booked"`
}

// InvoicePaymentsResponse wraps the Fortnox customer invoice payments list.
type InvoicePaymentsResponse struct {
	InvoicePayments []CustomerPaymentRow `json:"InvoicePayments"`
}

// ListCustomerInvoicePayments fetches payments for a specific customer invoice.
func (c *Client) ListCustomerInvoicePayments(invoiceNumber int) ([]CustomerPaymentRow, error) {
	url := fmt.Sprintf("%s/3/invoicepayments?filter=invoicenumber&invoicenumber=%d", c.baseURL, invoiceNumber)
	body, err := c.Get(url)
	if err != nil {
		return nil, fmt.Errorf("list customer invoice payments: %w", err)
	}
	var envelope InvoicePaymentsResponse
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("decode customer invoice payments: %w", err)
	}
	return envelope.InvoicePayments, nil
}

// CustomerRow has all customer fields relevant for analysis.
type CustomerRow struct {
	CustomerNumber string `json:"CustomerNumber"`
	Name           string `json:"Name"`
	Email          string `json:"Email"`
	Phone          string `json:"Phone1"`
	Active         bool   `json:"Active"`
}

// CustomerResponse wraps a customer detail response.
type CustomerResponse struct {
	Customer CustomerRow `json:"Customer"`
}

// GetCustomer fetches full customer details.
func (c *Client) GetCustomer(customerNumber int) (CustomerRow, error) {
	url := fmt.Sprintf("%s/3/customers/%d", c.baseURL, customerNumber)
	body, err := c.Get(url)
	if err != nil {
		return CustomerRow{}, fmt.Errorf("get customer %d: %w", customerNumber, err)
	}
	var envelope CustomerResponse
	if err := json.Unmarshal(body, &envelope); err != nil {
		return CustomerRow{}, fmt.Errorf("decode customer %d: %w", customerNumber, err)
	}
	return envelope.Customer, nil
}
```

- [ ] **Step 2: Write failing test for CustomerLedgerAdapter**

```go
// internal/adapter/fortnox/customer_ledger_test.go
package fortnox_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	adapterfortnox "github.com/mathiasb/cobalt-dingo/internal/adapter/fortnox"
)

func TestCustomerLedger_UnpaidInvoices(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"Invoices": []map[string]any{
				{
					"DocumentNumber": 2001,
					"CustomerNumber": "10",
					"CustomerName":   "Test AB",
					"Currency":       "SEK",
					"Total":          5000.0,
					"Balance":        5000.0,
					"DueDate":        "2026-05-01",
					"InvoiceDate":    "2026-04-01",
					"Booked":         true,
					"Cancelled":      false,
					"Sent":           true,
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	adapter := adapterfortnox.NewCustomerLedgerAdapter(srv.URL, stubTokenStore{})
	invoices, err := adapter.UnpaidInvoices(context.Background(), "test-tenant")
	if err != nil {
		t.Fatalf("UnpaidInvoices() error: %v", err)
	}
	if len(invoices) != 1 {
		t.Fatalf("expected 1 invoice, got %d", len(invoices))
	}
	if invoices[0].InvoiceNumber != 2001 {
		t.Errorf("expected invoice 2001, got %d", invoices[0].InvoiceNumber)
	}
	if invoices[0].CustomerName != "Test AB" {
		t.Errorf("expected customer Test AB, got %s", invoices[0].CustomerName)
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `cd /Users/mathias/Documents/local-dev/AGENTS/cobalt-dingo && go test ./internal/adapter/fortnox/... -run TestCustomerLedger -v`
Expected: FAIL — `NewCustomerLedgerAdapter` not defined

- [ ] **Step 4: Implement CustomerLedgerAdapter**

```go
// internal/adapter/fortnox/customer_ledger.go
package fortnox

import (
	"context"
	"fmt"
	"strconv"

	"github.com/mathiasb/cobalt-dingo/internal/domain"
	rawfortnox "github.com/mathiasb/cobalt-dingo/internal/fortnox"
)

// CustomerLedgerAdapter implements domain.CustomerLedger using the Fortnox API.
type CustomerLedgerAdapter struct {
	baseURL string
	tokens  domain.TokenStore
}

// NewCustomerLedgerAdapter creates a new CustomerLedgerAdapter.
func NewCustomerLedgerAdapter(baseURL string, tokens domain.TokenStore) *CustomerLedgerAdapter {
	return &CustomerLedgerAdapter{baseURL: baseURL, tokens: tokens}
}

func (a *CustomerLedgerAdapter) client(ctx context.Context, tenantID domain.TenantID) (*rawfortnox.Client, error) {
	tok, err := a.tokens.Load(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("load token: %w", err)
	}
	return rawfortnox.NewClient(a.baseURL, tok.AccessToken), nil
}

// UnpaidInvoices implements domain.CustomerLedger.
func (a *CustomerLedgerAdapter) UnpaidInvoices(ctx context.Context, tenantID domain.TenantID) ([]domain.CustomerInvoice, error) {
	c, err := a.client(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("customer ledger: %w", err)
	}
	rows, err := c.UnpaidCustomerInvoices()
	if err != nil {
		return nil, fmt.Errorf("customer ledger: %w", err)
	}
	invoices := make([]domain.CustomerInvoice, len(rows))
	for i, r := range rows {
		custNum, _ := strconv.Atoi(r.CustomerNumber)
		invoices[i] = domain.CustomerInvoice{
			InvoiceNumber:  r.DocumentNumber,
			CustomerNumber: custNum,
			CustomerName:   r.CustomerName,
			Amount:         domain.MoneyFromFloat(r.Total, r.Currency),
			Balance:        domain.MoneyFromFloat(r.Balance, r.Currency),
			DueDate:        r.DueDate,
			InvoiceDate:    r.InvoiceDate,
			Booked:         r.Booked,
			Cancelled:      r.Cancelled,
			Sent:           r.Sent,
		}
	}
	return invoices, nil
}

// InvoicePayments implements domain.CustomerLedger.
func (a *CustomerLedgerAdapter) InvoicePayments(ctx context.Context, tenantID domain.TenantID, invoiceNumber int) ([]domain.CustomerPayment, error) {
	c, err := a.client(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("customer ledger: %w", err)
	}
	rows, err := c.ListCustomerInvoicePayments(invoiceNumber)
	if err != nil {
		return nil, fmt.Errorf("customer invoice payments: %w", err)
	}
	payments := make([]domain.CustomerPayment, len(rows))
	for i, r := range rows {
		payments[i] = domain.CustomerPayment{
			PaymentNumber: r.Number,
			InvoiceNumber: r.InvoiceNumber,
			Amount:        domain.MoneyFromFloat(r.AmountCurrency, r.Currency),
			PaymentDate:   r.PaymentDate,
			Booked:        r.Booked,
		}
	}
	return payments, nil
}

// CustomerDetail implements domain.CustomerLedger.
func (a *CustomerLedgerAdapter) CustomerDetail(ctx context.Context, tenantID domain.TenantID, customerNumber int) (domain.Customer, error) {
	c, err := a.client(ctx, tenantID)
	if err != nil {
		return domain.Customer{}, fmt.Errorf("customer ledger: %w", err)
	}
	row, err := c.GetCustomer(customerNumber)
	if err != nil {
		return domain.Customer{}, fmt.Errorf("customer detail: %w", err)
	}
	custNum, _ := strconv.Atoi(row.CustomerNumber)
	return domain.Customer{
		CustomerNumber: custNum,
		Name:           row.Name,
		Email:          row.Email,
		Phone:          row.Phone,
		Active:         row.Active,
	}, nil
}
```

- [ ] **Step 5: Run tests**

Run: `cd /Users/mathias/Documents/local-dev/AGENTS/cobalt-dingo && go test ./internal/adapter/fortnox/... -run TestCustomerLedger -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/fortnox/customer.go internal/adapter/fortnox/customer_ledger.go internal/adapter/fortnox/customer_ledger_test.go
git commit -m "feat: add customer ledger adapter"
```

---

### Task 7: General ledger adapter

**Files:**
- Create: `internal/fortnox/ledger.go` — raw client methods for accounts, vouchers, financial years, predefined accounts
- Create: `internal/adapter/fortnox/general_ledger.go`
- Create: `internal/adapter/fortnox/general_ledger_test.go`

- [ ] **Step 1: Add raw client methods for GL endpoints**

```go
// internal/fortnox/ledger.go
package fortnox

import (
	"encoding/json"
	"fmt"
)

// AccountRow is the Fortnox JSON for a GL account.
type AccountRow struct {
	Number      int     `json:"Number"`
	Description string  `json:"Description"`
	SRU         int     `json:"SRU"`
	Active      bool    `json:"Active"`
	BalanceBF   float64 `json:"BalanceBroughtForward"`
	BalanceCF   float64 `json:"BalanceCarriedForward"`
}

type AccountsResponse struct {
	Accounts []AccountRow `json:"Accounts"`
}

// ListAccounts fetches the chart of accounts for a financial year.
func (c *Client) ListAccounts(yearID int) ([]AccountRow, error) {
	url := fmt.Sprintf("%s/3/accounts?financialyear=%d", c.baseURL, yearID)
	body, err := c.Get(url)
	if err != nil {
		return nil, fmt.Errorf("list accounts: %w", err)
	}
	var envelope AccountsResponse
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("decode accounts: %w", err)
	}
	return envelope.Accounts, nil
}

// VoucherRowJSON is a single line in a Fortnox voucher.
type VoucherRowJSON struct {
	Account           int     `json:"Account"`
	Debit             float64 `json:"Debit"`
	Credit            float64 `json:"Credit"`
	TransactionInformation string `json:"TransactionInformation"`
	CostCenter        string  `json:"CostCenter"`
	Project           string  `json:"Project"`
}

// VoucherJSON is a Fortnox journal entry.
type VoucherJSON struct {
	VoucherSeries   string           `json:"VoucherSeries"`
	VoucherNumber   int              `json:"VoucherNumber"`
	Description     string           `json:"Description"`
	TransactionDate string           `json:"TransactionDate"`
	Year            int              `json:"Year"`
	VoucherRows     []VoucherRowJSON `json:"VoucherRows"`
}

type VouchersResponse struct {
	Vouchers []VoucherJSON `json:"Vouchers"`
}

type VoucherDetailResponse struct {
	Voucher VoucherJSON `json:"Voucher"`
}

// ListVouchers fetches vouchers for a financial year.
func (c *Client) ListVouchers(yearID int) ([]VoucherJSON, error) {
	url := fmt.Sprintf("%s/3/vouchers?financialyear=%d", c.baseURL, yearID)
	body, err := c.Get(url)
	if err != nil {
		return nil, fmt.Errorf("list vouchers: %w", err)
	}
	var envelope VouchersResponse
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("decode vouchers: %w", err)
	}
	return envelope.Vouchers, nil
}

// GetVoucher fetches a single voucher with all rows.
func (c *Client) GetVoucher(series string, number int) (VoucherJSON, error) {
	url := fmt.Sprintf("%s/3/vouchers/%s/%d", c.baseURL, series, number)
	body, err := c.Get(url)
	if err != nil {
		return VoucherJSON{}, fmt.Errorf("get voucher: %w", err)
	}
	var envelope VoucherDetailResponse
	if err := json.Unmarshal(body, &envelope); err != nil {
		return VoucherJSON{}, fmt.Errorf("decode voucher: %w", err)
	}
	return envelope.Voucher, nil
}

// FinancialYearRow is the Fortnox JSON for a financial year.
type FinancialYearRow struct {
	ID       int    `json:"Id"`
	FromDate string `json:"FromDate"`
	ToDate   string `json:"ToDate"`
}

type FinancialYearsResponse struct {
	FinancialYears []FinancialYearRow `json:"FinancialYears"`
}

// ListFinancialYears fetches all financial years.
func (c *Client) ListFinancialYears() ([]FinancialYearRow, error) {
	body, err := c.Get(c.baseURL + "/3/financialyears")
	if err != nil {
		return nil, fmt.Errorf("list financial years: %w", err)
	}
	var envelope FinancialYearsResponse
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("decode financial years: %w", err)
	}
	return envelope.FinancialYears, nil
}

// PredefinedAccountRow is the Fortnox JSON for a predefined account.
type PredefinedAccountRow struct {
	Name    string `json:"Name"`
	Account int    `json:"Account"`
}

type PredefinedAccountsResponse struct {
	PreDefinedAccounts []PredefinedAccountRow `json:"PreDefinedAccounts"`
}

// ListPredefinedAccounts fetches system-defined account mappings.
func (c *Client) ListPredefinedAccounts() ([]PredefinedAccountRow, error) {
	body, err := c.Get(c.baseURL + "/3/predefinedaccounts")
	if err != nil {
		return nil, fmt.Errorf("list predefined accounts: %w", err)
	}
	var envelope PredefinedAccountsResponse
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("decode predefined accounts: %w", err)
	}
	return envelope.PreDefinedAccounts, nil
}
```

- [ ] **Step 2: Write failing test**

```go
// internal/adapter/fortnox/general_ledger_test.go
package fortnox_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	adapterfortnox "github.com/mathiasb/cobalt-dingo/internal/adapter/fortnox"
)

func TestGeneralLedger_ChartOfAccounts(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"Accounts": []map[string]any{
				{"Number": 1930, "Description": "Företagskonto", "SRU": 7510, "Active": true},
				{"Number": 2440, "Description": "Leverantörsskulder", "SRU": 7511, "Active": true},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	adapter := adapterfortnox.NewGeneralLedgerAdapter(srv.URL, stubTokenStore{})
	accounts, err := adapter.ChartOfAccounts(context.Background(), "test-tenant", 1)
	if err != nil {
		t.Fatalf("ChartOfAccounts() error: %v", err)
	}
	if len(accounts) != 2 {
		t.Fatalf("expected 2 accounts, got %d", len(accounts))
	}
	if accounts[0].Number != 1930 {
		t.Errorf("expected account 1930, got %d", accounts[0].Number)
	}
}

func TestGeneralLedger_FinancialYears(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"FinancialYears": []map[string]any{
				{"Id": 1, "FromDate": "2026-01-01", "ToDate": "2026-12-31"},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	adapter := adapterfortnox.NewGeneralLedgerAdapter(srv.URL, stubTokenStore{})
	years, err := adapter.FinancialYears(context.Background(), "test-tenant")
	if err != nil {
		t.Fatalf("FinancialYears() error: %v", err)
	}
	if len(years) != 1 {
		t.Fatalf("expected 1 year, got %d", len(years))
	}
	if years[0].ID != 1 {
		t.Errorf("expected year ID 1, got %d", years[0].ID)
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `cd /Users/mathias/Documents/local-dev/AGENTS/cobalt-dingo && go test ./internal/adapter/fortnox/... -run TestGeneralLedger -v`
Expected: FAIL

- [ ] **Step 4: Implement GeneralLedgerAdapter**

```go
// internal/adapter/fortnox/general_ledger.go
package fortnox

import (
	"context"
	"fmt"
	"time"

	"github.com/mathiasb/cobalt-dingo/internal/domain"
	rawfortnox "github.com/mathiasb/cobalt-dingo/internal/fortnox"
)

// GeneralLedgerAdapter implements domain.GeneralLedger using the Fortnox API.
type GeneralLedgerAdapter struct {
	baseURL string
	tokens  domain.TokenStore
}

// NewGeneralLedgerAdapter creates a new GeneralLedgerAdapter.
func NewGeneralLedgerAdapter(baseURL string, tokens domain.TokenStore) *GeneralLedgerAdapter {
	return &GeneralLedgerAdapter{baseURL: baseURL, tokens: tokens}
}

func (a *GeneralLedgerAdapter) client(ctx context.Context, tenantID domain.TenantID) (*rawfortnox.Client, error) {
	tok, err := a.tokens.Load(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("load token: %w", err)
	}
	return rawfortnox.NewClient(a.baseURL, tok.AccessToken), nil
}

// ChartOfAccounts implements domain.GeneralLedger.
func (a *GeneralLedgerAdapter) ChartOfAccounts(ctx context.Context, tenantID domain.TenantID, yearID int) ([]domain.Account, error) {
	c, err := a.client(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("general ledger: %w", err)
	}
	rows, err := c.ListAccounts(yearID)
	if err != nil {
		return nil, fmt.Errorf("chart of accounts: %w", err)
	}
	accounts := make([]domain.Account, len(rows))
	for i, r := range rows {
		accounts[i] = domain.Account{
			Number:      r.Number,
			Description: r.Description,
			SRU:         r.SRU,
			Active:      r.Active,
			Year:        yearID,
			BalanceBF:   domain.MoneyFromFloat(r.BalanceBF, "SEK"),
			BalanceCF:   domain.MoneyFromFloat(r.BalanceCF, "SEK"),
		}
	}
	return accounts, nil
}

// AccountBalances implements domain.GeneralLedger.
// Uses the chart of accounts endpoint filtered to the account range.
func (a *GeneralLedgerAdapter) AccountBalances(ctx context.Context, tenantID domain.TenantID, yearID int, fromAcct, toAcct int) ([]domain.AccountBalance, error) {
	accounts, err := a.ChartOfAccounts(ctx, tenantID, yearID)
	if err != nil {
		return nil, err
	}
	var balances []domain.AccountBalance
	for _, acct := range accounts {
		if acct.Number >= fromAcct && acct.Number <= toAcct && acct.BalanceCF.MinorUnits != 0 {
			balances = append(balances, domain.AccountBalance{
				AccountNumber: acct.Number,
				Balance:       acct.BalanceCF,
			})
		}
	}
	return balances, nil
}

// AccountActivity implements domain.GeneralLedger.
// Fetches vouchers and filters rows to the specified account.
func (a *GeneralLedgerAdapter) AccountActivity(ctx context.Context, tenantID domain.TenantID, yearID int, acctNum int, from, to time.Time) ([]domain.VoucherRow, error) {
	vouchers, err := a.Vouchers(ctx, tenantID, yearID, from, to)
	if err != nil {
		return nil, err
	}
	var rows []domain.VoucherRow
	for _, v := range vouchers {
		for _, r := range v.Rows {
			if r.Account == acctNum {
				rows = append(rows, r)
			}
		}
	}
	return rows, nil
}

// Vouchers implements domain.GeneralLedger.
func (a *GeneralLedgerAdapter) Vouchers(ctx context.Context, tenantID domain.TenantID, yearID int, from, to time.Time) ([]domain.Voucher, error) {
	c, err := a.client(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("general ledger: %w", err)
	}
	rows, err := c.ListVouchers(yearID)
	if err != nil {
		return nil, fmt.Errorf("list vouchers: %w", err)
	}
	var vouchers []domain.Voucher
	for _, r := range rows {
		txDate, _ := time.Parse("2006-01-02", r.TransactionDate)
		if !from.IsZero() && txDate.Before(from) {
			continue
		}
		if !to.IsZero() && txDate.After(to) {
			continue
		}
		v := domain.Voucher{
			Series:          r.VoucherSeries,
			Number:          r.VoucherNumber,
			Description:     r.Description,
			TransactionDate: r.TransactionDate,
			Year:            r.Year,
		}
		for _, vr := range r.VoucherRows {
			v.Rows = append(v.Rows, domain.VoucherRow{
				Account:     vr.Account,
				Debit:       domain.MoneyFromFloat(vr.Debit, "SEK"),
				Credit:      domain.MoneyFromFloat(vr.Credit, "SEK"),
				Description: vr.TransactionInformation,
				CostCenter:  vr.CostCenter,
				Project:     vr.Project,
			})
		}
		vouchers = append(vouchers, v)
	}
	return vouchers, nil
}

// VoucherDetail implements domain.GeneralLedger.
func (a *GeneralLedgerAdapter) VoucherDetail(ctx context.Context, tenantID domain.TenantID, series string, number int) (domain.Voucher, error) {
	c, err := a.client(ctx, tenantID)
	if err != nil {
		return domain.Voucher{}, fmt.Errorf("general ledger: %w", err)
	}
	r, err := c.GetVoucher(series, number)
	if err != nil {
		return domain.Voucher{}, fmt.Errorf("voucher detail: %w", err)
	}
	v := domain.Voucher{
		Series:          r.VoucherSeries,
		Number:          r.VoucherNumber,
		Description:     r.Description,
		TransactionDate: r.TransactionDate,
		Year:            r.Year,
	}
	for _, vr := range r.VoucherRows {
		v.Rows = append(v.Rows, domain.VoucherRow{
			Account:     vr.Account,
			Debit:       domain.MoneyFromFloat(vr.Debit, "SEK"),
			Credit:      domain.MoneyFromFloat(vr.Credit, "SEK"),
			Description: vr.TransactionInformation,
			CostCenter:  vr.CostCenter,
			Project:     vr.Project,
		})
	}
	return v, nil
}

// FinancialYears implements domain.GeneralLedger.
func (a *GeneralLedgerAdapter) FinancialYears(ctx context.Context, tenantID domain.TenantID) ([]domain.FinancialYear, error) {
	c, err := a.client(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("general ledger: %w", err)
	}
	rows, err := c.ListFinancialYears()
	if err != nil {
		return nil, fmt.Errorf("financial years: %w", err)
	}
	years := make([]domain.FinancialYear, len(rows))
	for i, r := range rows {
		from, _ := time.Parse("2006-01-02", r.FromDate)
		to, _ := time.Parse("2006-01-02", r.ToDate)
		years[i] = domain.FinancialYear{ID: r.ID, From: from, To: to}
	}
	return years, nil
}

// PredefinedAccounts implements domain.GeneralLedger.
func (a *GeneralLedgerAdapter) PredefinedAccounts(ctx context.Context, tenantID domain.TenantID) ([]domain.PredefinedAccount, error) {
	c, err := a.client(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("general ledger: %w", err)
	}
	rows, err := c.ListPredefinedAccounts()
	if err != nil {
		return nil, fmt.Errorf("predefined accounts: %w", err)
	}
	accts := make([]domain.PredefinedAccount, len(rows))
	for i, r := range rows {
		accts[i] = domain.PredefinedAccount{Name: r.Name, Account: r.Account}
	}
	return accts, nil
}
```

- [ ] **Step 5: Run tests**

Run: `cd /Users/mathias/Documents/local-dev/AGENTS/cobalt-dingo && go test ./internal/adapter/fortnox/... -run TestGeneralLedger -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/fortnox/ledger.go internal/adapter/fortnox/general_ledger.go internal/adapter/fortnox/general_ledger_test.go
git commit -m "feat: add general ledger adapter"
```

---

### Task 8: Project ledger adapter

**Files:**
- Create: `internal/fortnox/project.go`
- Create: `internal/adapter/fortnox/project_ledger.go`
- Create: `internal/adapter/fortnox/project_ledger_test.go`

- [ ] **Step 1: Add raw client methods for projects**

```go
// internal/fortnox/project.go
package fortnox

import (
	"encoding/json"
	"fmt"
)

// ProjectRow is the Fortnox JSON for a project.
type ProjectRow struct {
	ProjectNumber string `json:"ProjectNumber"`
	Description   string `json:"Description"`
	Status        string `json:"Status"`
	StartDate     string `json:"StartDate"`
	EndDate       string `json:"EndDate"`
}

type ProjectsResponse struct {
	Projects []ProjectRow `json:"Projects"`
}

// ListProjects fetches all projects.
func (c *Client) ListProjects() ([]ProjectRow, error) {
	body, err := c.Get(c.baseURL + "/3/projects")
	if err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}
	var envelope ProjectsResponse
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("decode projects: %w", err)
	}
	return envelope.Projects, nil
}
```

- [ ] **Step 2: Write failing test**

```go
// internal/adapter/fortnox/project_ledger_test.go
package fortnox_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	adapterfortnox "github.com/mathiasb/cobalt-dingo/internal/adapter/fortnox"
)

func TestProjectLedger_Projects(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"Projects": []map[string]any{
				{"ProjectNumber": "P001", "Description": "Website Redesign", "Status": "ONGOING"},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	adapter := adapterfortnox.NewProjectLedgerAdapter(srv.URL, stubTokenStore{})
	projects, err := adapter.Projects(context.Background(), "test-tenant")
	if err != nil {
		t.Fatalf("Projects() error: %v", err)
	}
	if len(projects) != 1 {
		t.Fatalf("expected 1 project, got %d", len(projects))
	}
	if projects[0].Number != "P001" {
		t.Errorf("expected P001, got %s", projects[0].Number)
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `cd /Users/mathias/Documents/local-dev/AGENTS/cobalt-dingo && go test ./internal/adapter/fortnox/... -run TestProjectLedger -v`
Expected: FAIL

- [ ] **Step 4: Implement ProjectLedgerAdapter**

```go
// internal/adapter/fortnox/project_ledger.go
package fortnox

import (
	"context"
	"fmt"
	"time"

	"github.com/mathiasb/cobalt-dingo/internal/domain"
	rawfortnox "github.com/mathiasb/cobalt-dingo/internal/fortnox"
)

// ProjectLedgerAdapter implements domain.ProjectLedger using the Fortnox API.
type ProjectLedgerAdapter struct {
	baseURL string
	tokens  domain.TokenStore
	gl      *GeneralLedgerAdapter // reuse GL for voucher queries
}

// NewProjectLedgerAdapter creates a new ProjectLedgerAdapter.
// It requires a GeneralLedgerAdapter to query vouchers filtered by project.
func NewProjectLedgerAdapter(baseURL string, tokens domain.TokenStore, gl *GeneralLedgerAdapter) *ProjectLedgerAdapter {
	return &ProjectLedgerAdapter{baseURL: baseURL, tokens: tokens, gl: gl}
}

func (a *ProjectLedgerAdapter) client(ctx context.Context, tenantID domain.TenantID) (*rawfortnox.Client, error) {
	tok, err := a.tokens.Load(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("load token: %w", err)
	}
	return rawfortnox.NewClient(a.baseURL, tok.AccessToken), nil
}

// Projects implements domain.ProjectLedger.
func (a *ProjectLedgerAdapter) Projects(ctx context.Context, tenantID domain.TenantID) ([]domain.Project, error) {
	c, err := a.client(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("project ledger: %w", err)
	}
	rows, err := c.ListProjects()
	if err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}
	projects := make([]domain.Project, len(rows))
	for i, r := range rows {
		projects[i] = domain.Project{
			Number:      r.ProjectNumber,
			Description: r.Description,
			Status:      r.Status,
			StartDate:   r.StartDate,
			EndDate:     r.EndDate,
		}
	}
	return projects, nil
}

// ProjectTransactions implements domain.ProjectLedger.
// Fetches all vouchers in the period, then filters rows by project.
func (a *ProjectLedgerAdapter) ProjectTransactions(ctx context.Context, tenantID domain.TenantID, projectID string, from, to time.Time) ([]domain.VoucherRow, error) {
	// Determine the financial year from the from date.
	years, err := a.gl.FinancialYears(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("project transactions: %w", err)
	}
	yearID := 0
	for _, y := range years {
		if !from.Before(y.From) && !from.After(y.To) {
			yearID = y.ID
			break
		}
	}
	if yearID == 0 && len(years) > 0 {
		yearID = years[len(years)-1].ID
	}

	vouchers, err := a.gl.Vouchers(ctx, tenantID, yearID, from, to)
	if err != nil {
		return nil, fmt.Errorf("project transactions: %w", err)
	}
	var rows []domain.VoucherRow
	for _, v := range vouchers {
		for _, r := range v.Rows {
			if r.Project == projectID {
				rows = append(rows, r)
			}
		}
	}
	return rows, nil
}
```

- [ ] **Step 5: Run tests**

Run: `cd /Users/mathias/Documents/local-dev/AGENTS/cobalt-dingo && go test ./internal/adapter/fortnox/... -run TestProjectLedger -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/fortnox/project.go internal/adapter/fortnox/project_ledger.go internal/adapter/fortnox/project_ledger_test.go
git commit -m "feat: add project ledger adapter"
```

---

### Task 9: Cost center ledger adapter

**Files:**
- Create: `internal/fortnox/costcenter.go`
- Create: `internal/adapter/fortnox/costcenter_ledger.go`
- Create: `internal/adapter/fortnox/costcenter_ledger_test.go`

- [ ] **Step 1: Add raw client methods**

```go
// internal/fortnox/costcenter.go
package fortnox

import (
	"encoding/json"
	"fmt"
)

// CostCenterRow is the Fortnox JSON for a cost center.
type CostCenterRow struct {
	Code        string `json:"Code"`
	Description string `json:"Description"`
	Active      bool   `json:"Active"`
}

type CostCentersResponse struct {
	CostCenters []CostCenterRow `json:"CostCenters"`
}

// ListCostCenters fetches all cost centers.
func (c *Client) ListCostCenters() ([]CostCenterRow, error) {
	body, err := c.Get(c.baseURL + "/3/costcenters")
	if err != nil {
		return nil, fmt.Errorf("list cost centers: %w", err)
	}
	var envelope CostCentersResponse
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("decode cost centers: %w", err)
	}
	return envelope.CostCenters, nil
}
```

- [ ] **Step 2: Write failing test**

```go
// internal/adapter/fortnox/costcenter_ledger_test.go
package fortnox_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	adapterfortnox "github.com/mathiasb/cobalt-dingo/internal/adapter/fortnox"
)

func TestCostCenterLedger_CostCenters(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"CostCenters": []map[string]any{
				{"Code": "100", "Description": "Sales", "Active": true},
				{"Code": "200", "Description": "Engineering", "Active": true},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	adapter := adapterfortnox.NewCostCenterLedgerAdapter(srv.URL, stubTokenStore{}, nil)
	centers, err := adapter.CostCenters(context.Background(), "test-tenant")
	if err != nil {
		t.Fatalf("CostCenters() error: %v", err)
	}
	if len(centers) != 2 {
		t.Fatalf("expected 2 cost centers, got %d", len(centers))
	}
}
```

- [ ] **Step 3: Implement CostCenterLedgerAdapter**

```go
// internal/adapter/fortnox/costcenter_ledger.go
package fortnox

import (
	"context"
	"fmt"
	"time"

	"github.com/mathiasb/cobalt-dingo/internal/domain"
	rawfortnox "github.com/mathiasb/cobalt-dingo/internal/fortnox"
)

// CostCenterLedgerAdapter implements domain.CostCenterLedger using the Fortnox API.
type CostCenterLedgerAdapter struct {
	baseURL string
	tokens  domain.TokenStore
	gl      *GeneralLedgerAdapter
}

// NewCostCenterLedgerAdapter creates a new CostCenterLedgerAdapter.
func NewCostCenterLedgerAdapter(baseURL string, tokens domain.TokenStore, gl *GeneralLedgerAdapter) *CostCenterLedgerAdapter {
	return &CostCenterLedgerAdapter{baseURL: baseURL, tokens: tokens, gl: gl}
}

func (a *CostCenterLedgerAdapter) client(ctx context.Context, tenantID domain.TenantID) (*rawfortnox.Client, error) {
	tok, err := a.tokens.Load(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("load token: %w", err)
	}
	return rawfortnox.NewClient(a.baseURL, tok.AccessToken), nil
}

// CostCenters implements domain.CostCenterLedger.
func (a *CostCenterLedgerAdapter) CostCenters(ctx context.Context, tenantID domain.TenantID) ([]domain.CostCenter, error) {
	c, err := a.client(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("cost center ledger: %w", err)
	}
	rows, err := c.ListCostCenters()
	if err != nil {
		return nil, fmt.Errorf("list cost centers: %w", err)
	}
	centers := make([]domain.CostCenter, len(rows))
	for i, r := range rows {
		centers[i] = domain.CostCenter{
			Code:        r.Code,
			Description: r.Description,
			Active:      r.Active,
		}
	}
	return centers, nil
}

// CostCenterTransactions implements domain.CostCenterLedger.
func (a *CostCenterLedgerAdapter) CostCenterTransactions(ctx context.Context, tenantID domain.TenantID, code string, from, to time.Time) ([]domain.VoucherRow, error) {
	years, err := a.gl.FinancialYears(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("cost center transactions: %w", err)
	}
	yearID := 0
	for _, y := range years {
		if !from.Before(y.From) && !from.After(y.To) {
			yearID = y.ID
			break
		}
	}
	if yearID == 0 && len(years) > 0 {
		yearID = years[len(years)-1].ID
	}

	vouchers, err := a.gl.Vouchers(ctx, tenantID, yearID, from, to)
	if err != nil {
		return nil, fmt.Errorf("cost center transactions: %w", err)
	}
	var rows []domain.VoucherRow
	for _, v := range vouchers {
		for _, r := range v.Rows {
			if r.CostCenter == code {
				rows = append(rows, r)
			}
		}
	}
	return rows, nil
}
```

- [ ] **Step 4: Run tests**

Run: `cd /Users/mathias/Documents/local-dev/AGENTS/cobalt-dingo && go test ./internal/adapter/fortnox/... -run TestCostCenterLedger -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/fortnox/costcenter.go internal/adapter/fortnox/costcenter_ledger.go internal/adapter/fortnox/costcenter_ledger_test.go
git commit -m "feat: add cost center ledger adapter"
```

---

### Task 10: Asset register adapter

**Files:**
- Create: `internal/fortnox/asset.go`
- Create: `internal/adapter/fortnox/asset_register.go`
- Create: `internal/adapter/fortnox/asset_register_test.go`

- [ ] **Step 1: Add raw client methods**

```go
// internal/fortnox/asset.go
package fortnox

import (
	"encoding/json"
	"fmt"
)

// AssetRow is the Fortnox JSON for a fixed asset.
type AssetRow struct {
	ID                  int     `json:"Id"`
	Number              string  `json:"Number"`
	Description         string  `json:"Description"`
	AcquisitionDate     string  `json:"AcquisitionDate"`
	AcquisitionValue    float64 `json:"AcquisitionValue"`
	DepreciationMethod  string  `json:"DepreciationMethod"`
	DepreciateToResidualValue float64 `json:"DepreciateToResidualValue"`
	BookValue           float64 `json:"BookValue"`
	AccumulatedDepreciation float64 `json:"AccumulatedDepreciation"`
}

type AssetsResponse struct {
	Assets []AssetRow `json:"Assets"`
}

type AssetDetailResponse struct {
	Asset AssetRow `json:"Asset"`
}

// ListAssets fetches all fixed assets.
func (c *Client) ListAssets() ([]AssetRow, error) {
	body, err := c.Get(c.baseURL + "/3/assets")
	if err != nil {
		return nil, fmt.Errorf("list assets: %w", err)
	}
	var envelope AssetsResponse
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("decode assets: %w", err)
	}
	return envelope.Assets, nil
}

// GetAsset fetches a single asset by ID.
func (c *Client) GetAsset(assetID int) (AssetRow, error) {
	url := fmt.Sprintf("%s/3/assets/%d", c.baseURL, assetID)
	body, err := c.Get(url)
	if err != nil {
		return AssetRow{}, fmt.Errorf("get asset %d: %w", assetID, err)
	}
	var envelope AssetDetailResponse
	if err := json.Unmarshal(body, &envelope); err != nil {
		return AssetRow{}, fmt.Errorf("decode asset %d: %w", assetID, err)
	}
	return envelope.Asset, nil
}
```

- [ ] **Step 2: Write failing test**

```go
// internal/adapter/fortnox/asset_register_test.go
package fortnox_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	adapterfortnox "github.com/mathiasb/cobalt-dingo/internal/adapter/fortnox"
)

func TestAssetRegister_Assets(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"Assets": []map[string]any{
				{
					"Id": 1, "Number": "A001", "Description": "MacBook Pro",
					"AcquisitionDate": "2025-01-15", "AcquisitionValue": 25000.0,
					"DepreciationMethod": "STRAIGHT_LINE", "BookValue": 20000.0,
					"AccumulatedDepreciation": 5000.0,
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	adapter := adapterfortnox.NewAssetRegisterAdapter(srv.URL, stubTokenStore{})
	assets, err := adapter.Assets(context.Background(), "test-tenant")
	if err != nil {
		t.Fatalf("Assets() error: %v", err)
	}
	if len(assets) != 1 {
		t.Fatalf("expected 1 asset, got %d", len(assets))
	}
	if assets[0].Description != "MacBook Pro" {
		t.Errorf("expected MacBook Pro, got %s", assets[0].Description)
	}
}
```

- [ ] **Step 3: Implement AssetRegisterAdapter**

```go
// internal/adapter/fortnox/asset_register.go
package fortnox

import (
	"context"
	"fmt"

	"github.com/mathiasb/cobalt-dingo/internal/domain"
	rawfortnox "github.com/mathiasb/cobalt-dingo/internal/fortnox"
)

// AssetRegisterAdapter implements domain.AssetRegister using the Fortnox API.
type AssetRegisterAdapter struct {
	baseURL string
	tokens  domain.TokenStore
}

// NewAssetRegisterAdapter creates a new AssetRegisterAdapter.
func NewAssetRegisterAdapter(baseURL string, tokens domain.TokenStore) *AssetRegisterAdapter {
	return &AssetRegisterAdapter{baseURL: baseURL, tokens: tokens}
}

func (a *AssetRegisterAdapter) client(ctx context.Context, tenantID domain.TenantID) (*rawfortnox.Client, error) {
	tok, err := a.tokens.Load(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("load token: %w", err)
	}
	return rawfortnox.NewClient(a.baseURL, tok.AccessToken), nil
}

// Assets implements domain.AssetRegister.
func (a *AssetRegisterAdapter) Assets(ctx context.Context, tenantID domain.TenantID) ([]domain.Asset, error) {
	c, err := a.client(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("asset register: %w", err)
	}
	rows, err := c.ListAssets()
	if err != nil {
		return nil, fmt.Errorf("list assets: %w", err)
	}
	assets := make([]domain.Asset, len(rows))
	for i, r := range rows {
		assets[i] = rowToAsset(r)
	}
	return assets, nil
}

// AssetDetail implements domain.AssetRegister.
func (a *AssetRegisterAdapter) AssetDetail(ctx context.Context, tenantID domain.TenantID, assetID int) (domain.Asset, error) {
	c, err := a.client(ctx, tenantID)
	if err != nil {
		return domain.Asset{}, fmt.Errorf("asset register: %w", err)
	}
	r, err := c.GetAsset(assetID)
	if err != nil {
		return domain.Asset{}, fmt.Errorf("asset detail: %w", err)
	}
	return rowToAsset(r), nil
}

func rowToAsset(r rawfortnox.AssetRow) domain.Asset {
	return domain.Asset{
		ID:                 r.ID,
		Number:             r.Number,
		Description:        r.Description,
		AcquisitionDate:    r.AcquisitionDate,
		AcquisitionValue:   domain.MoneyFromFloat(r.AcquisitionValue, "SEK"),
		DepreciationMethod: r.DepreciationMethod,
		BookValue:          domain.MoneyFromFloat(r.BookValue, "SEK"),
		AccumulatedDepr:    domain.MoneyFromFloat(r.AccumulatedDepreciation, "SEK"),
	}
}
```

- [ ] **Step 4: Run tests**

Run: `cd /Users/mathias/Documents/local-dev/AGENTS/cobalt-dingo && go test ./internal/adapter/fortnox/... -run TestAssetRegister -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/fortnox/asset.go internal/adapter/fortnox/asset_register.go internal/adapter/fortnox/asset_register_test.go
git commit -m "feat: add asset register adapter"
```

---

### Task 11: Company info adapter

**Files:**
- Create: `internal/fortnox/company.go`
- Create: `internal/adapter/fortnox/company.go`
- Create: `internal/adapter/fortnox/company_test.go`

- [ ] **Step 1: Add raw client method**

```go
// internal/fortnox/company.go
package fortnox

import (
	"encoding/json"
	"fmt"
)

// CompanyInfoRow is the Fortnox JSON for company information.
type CompanyInfoRow struct {
	CompanyName    string `json:"CompanyName"`
	OrganizationNumber string `json:"OrganizationNumber"`
	Address        string `json:"Address"`
	City           string `json:"City"`
	ZipCode        string `json:"ZipCode"`
	Country        string `json:"Country"`
	Email          string `json:"Email"`
	Phone          string `json:"Phone1"`
	VisitAddress   string `json:"VisitAddress"`
	VisitCity      string `json:"VisitCity"`
	VisitZipCode   string `json:"VisitZipCode"`
}

type CompanyInfoResponse struct {
	CompanyInformation CompanyInfoRow `json:"CompanyInformation"`
}

// GetCompanyInfo fetches company profile information.
func (c *Client) GetCompanyInfo() (CompanyInfoRow, error) {
	body, err := c.Get(c.baseURL + "/3/companyinformation")
	if err != nil {
		return CompanyInfoRow{}, fmt.Errorf("get company info: %w", err)
	}
	var envelope CompanyInfoResponse
	if err := json.Unmarshal(body, &envelope); err != nil {
		return CompanyInfoRow{}, fmt.Errorf("decode company info: %w", err)
	}
	return envelope.CompanyInformation, nil
}
```

- [ ] **Step 2: Write failing test**

```go
// internal/adapter/fortnox/company_test.go
package fortnox_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	adapterfortnox "github.com/mathiasb/cobalt-dingo/internal/adapter/fortnox"
)

func TestCompanyInfo_Info(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"CompanyInformation": map[string]any{
				"CompanyName":        "Test AB",
				"OrganizationNumber": "556677-8899",
				"City":               "Stockholm",
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	adapter := adapterfortnox.NewCompanyInfoAdapter(srv.URL, stubTokenStore{})
	info, err := adapter.Info(context.Background(), "test-tenant")
	if err != nil {
		t.Fatalf("Info() error: %v", err)
	}
	if info.Name != "Test AB" {
		t.Errorf("expected Test AB, got %s", info.Name)
	}
	if info.OrgNumber != "556677-8899" {
		t.Errorf("expected 556677-8899, got %s", info.OrgNumber)
	}
}
```

- [ ] **Step 3: Implement CompanyInfoAdapter**

```go
// internal/adapter/fortnox/company.go
package fortnox

import (
	"context"
	"fmt"

	"github.com/mathiasb/cobalt-dingo/internal/domain"
	rawfortnox "github.com/mathiasb/cobalt-dingo/internal/fortnox"
)

// CompanyInfoAdapter implements domain.CompanyInfo using the Fortnox API.
type CompanyInfoAdapter struct {
	baseURL string
	tokens  domain.TokenStore
}

// NewCompanyInfoAdapter creates a new CompanyInfoAdapter.
func NewCompanyInfoAdapter(baseURL string, tokens domain.TokenStore) *CompanyInfoAdapter {
	return &CompanyInfoAdapter{baseURL: baseURL, tokens: tokens}
}

func (a *CompanyInfoAdapter) client(ctx context.Context, tenantID domain.TenantID) (*rawfortnox.Client, error) {
	tok, err := a.tokens.Load(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("load token: %w", err)
	}
	return rawfortnox.NewClient(a.baseURL, tok.AccessToken), nil
}

// Info implements domain.CompanyInfo.
func (a *CompanyInfoAdapter) Info(ctx context.Context, tenantID domain.TenantID) (domain.Company, error) {
	c, err := a.client(ctx, tenantID)
	if err != nil {
		return domain.Company{}, fmt.Errorf("company info: %w", err)
	}
	row, err := c.GetCompanyInfo()
	if err != nil {
		return domain.Company{}, fmt.Errorf("company info: %w", err)
	}
	return domain.Company{
		Name:         row.CompanyName,
		OrgNumber:    row.OrganizationNumber,
		Address:      row.Address,
		City:         row.City,
		ZipCode:      row.ZipCode,
		Country:      row.Country,
		Email:        row.Email,
		Phone:        row.Phone,
		VisitAddress: row.VisitAddress,
		VisitCity:    row.VisitCity,
		VisitZipCode: row.VisitZipCode,
	}, nil
}
```

- [ ] **Step 4: Run tests**

Run: `cd /Users/mathias/Documents/local-dev/AGENTS/cobalt-dingo && go test ./internal/adapter/fortnox/... -run TestCompanyInfo -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/fortnox/company.go internal/adapter/fortnox/company.go internal/adapter/fortnox/company_test.go
git commit -m "feat: add company info adapter"
```

---

## Phase 3: MCP Server

### Task 12: MCP server scaffold and AP tools

**Files:**
- Create: `cmd/mcp/main.go`
- Create: `internal/mcp/server.go`
- Create: `internal/mcp/tools_ap.go`

- [ ] **Step 1: Add mcp-go dependency**

Run: `cd /Users/mathias/Documents/local-dev/AGENTS/cobalt-dingo && go get github.com/mark3labs/mcp-go@latest`

- [ ] **Step 2: Create MCP server setup**

```go
// internal/mcp/server.go
package mcp

import (
	"github.com/mark3labs/mcp-go/mcp"
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

// NewServer creates an MCP server with all 38 tools registered.
func NewServer(deps Deps) *server.MCPServer {
	s := server.NewMCPServer(
		"cobalt-dingo",
		"0.5.0",
		server.WithToolCapabilities(true),
	)

	registerAPTools(s, deps)
	// registerARTools(s, deps)       — Task 13
	// registerGLTools(s, deps)       — Task 14
	// registerProjectTools(s, deps)  — Task 15
	// registerCostCtrTools(s, deps)  — Task 15
	// registerAssetTools(s, deps)    — Task 15
	// registerAnalyticsTools(s, deps) — Task 16

	return s
}
```

- [ ] **Step 3: Create AP tool handlers**

```go
// internal/mcp/tools_ap.go
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/mathiasb/cobalt-dingo/internal/analyst"
	"github.com/mathiasb/cobalt-dingo/internal/domain"
)

func registerAPTools(s *server.MCPServer, deps Deps) {
	s.AddTool(mcp.NewTool("ap_summary",
		mcp.WithDescription("Total unpaid supplier invoice count, total by currency, oldest due date"),
	), apSummaryHandler(deps))

	s.AddTool(mcp.NewTool("ap_overdue",
		mcp.WithDescription("Overdue supplier invoices with days past due"),
		mcp.WithNumber("days_threshold", mcp.Description("Minimum days overdue (default 0)")),
	), apOverdueHandler(deps))

	s.AddTool(mcp.NewTool("ap_by_supplier",
		mcp.WithDescription("Unpaid supplier invoice totals grouped by supplier"),
		mcp.WithNumber("limit", mcp.Description("Max suppliers to return, sorted by amount desc")),
	), apBySupplierHandler(deps))

	s.AddTool(mcp.NewTool("ap_by_currency",
		mcp.WithDescription("Unpaid supplier invoice totals grouped by currency"),
	), apByCurrencyHandler(deps))

	s.AddTool(mcp.NewTool("ap_aging",
		mcp.WithDescription("Supplier invoice aging report: current, 1-30, 31-60, 61-90, 90+ days"),
	), apAgingHandler(deps))

	s.AddTool(mcp.NewTool("ap_supplier_history",
		mcp.WithDescription("All invoices and payments for a specific supplier"),
		mcp.WithNumber("supplier_number", mcp.Required(), mcp.Description("Fortnox supplier number")),
	), apSupplierHistoryHandler(deps))

	s.AddTool(mcp.NewTool("ap_invoice_detail",
		mcp.WithDescription("Single supplier invoice with payment history"),
		mcp.WithNumber("invoice_number", mcp.Required(), mcp.Description("Fortnox invoice number")),
	), apInvoiceDetailHandler(deps))
}

func apSummaryHandler(deps Deps) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		invoices, err := deps.SupplierLdg.UnpaidInvoices(ctx, deps.TenantID)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("fetch invoices: %v", err)), nil
		}

		byCurrency := analyst.GroupBy(invoices, func(i domain.SupplierInvoice) string { return i.Amount.Currency })
		type currencyTotal struct {
			Currency string  `json:"currency"`
			Count    int     `json:"count"`
			Total    float64 `json:"total"`
		}
		var totals []currencyTotal
		for ccy, invs := range byCurrency {
			var sum int64
			for _, inv := range invs {
				sum += inv.Amount.MinorUnits
			}
			totals = append(totals, currencyTotal{Currency: ccy, Count: len(invs), Total: float64(sum) / 100})
		}

		var oldestDue string
		for _, inv := range invoices {
			if oldestDue == "" || inv.DueDate < oldestDue {
				oldestDue = inv.DueDate
			}
		}

		result := map[string]any{
			"total_count":   len(invoices),
			"by_currency":   totals,
			"oldest_due_date": oldestDue,
		}
		return jsonResult(result)
	}
}

func apOverdueHandler(deps Deps) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		threshold := int(req.GetArgFloat("days_threshold", 0))
		invoices, err := deps.SupplierLdg.UnpaidInvoices(ctx, deps.TenantID)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("fetch invoices: %v", err)), nil
		}

		today := time.Now()
		type overdueInvoice struct {
			InvoiceNumber  int     `json:"invoice_number"`
			SupplierName   string  `json:"supplier_name"`
			Amount         float64 `json:"amount"`
			Currency       string  `json:"currency"`
			DueDate        string  `json:"due_date"`
			DaysOverdue    int     `json:"days_overdue"`
		}
		var overdue []overdueInvoice
		for _, inv := range invoices {
			days := analyst.DaysOverdue(inv.DueDate, today)
			if days > threshold {
				overdue = append(overdue, overdueInvoice{
					InvoiceNumber: inv.InvoiceNumber,
					SupplierName:  inv.SupplierName,
					Amount:        inv.Amount.Float(),
					Currency:      inv.Amount.Currency,
					DueDate:       inv.DueDate,
					DaysOverdue:   days,
				})
			}
		}
		sort.Slice(overdue, func(i, j int) bool { return overdue[i].DaysOverdue > overdue[j].DaysOverdue })

		return jsonResult(map[string]any{"count": len(overdue), "invoices": overdue})
	}
}

func apBySupplierHandler(deps Deps) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		limit := int(req.GetArgFloat("limit", 0))
		invoices, err := deps.SupplierLdg.UnpaidInvoices(ctx, deps.TenantID)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("fetch invoices: %v", err)), nil
		}

		type supplierTotal struct {
			SupplierNumber int     `json:"supplier_number"`
			SupplierName   string  `json:"supplier_name"`
			Count          int     `json:"count"`
			TotalSEK       float64 `json:"total_sek"`
		}
		bySupplier := analyst.GroupBy(invoices, func(i domain.SupplierInvoice) int { return i.SupplierNumber })
		var totals []supplierTotal
		for _, invs := range bySupplier {
			var sum int64
			for _, inv := range invs {
				sum += inv.Amount.MinorUnits
			}
			totals = append(totals, supplierTotal{
				SupplierNumber: invs[0].SupplierNumber,
				SupplierName:   invs[0].SupplierName,
				Count:          len(invs),
				TotalSEK:       float64(sum) / 100,
			})
		}
		sort.Slice(totals, func(i, j int) bool { return totals[i].TotalSEK > totals[j].TotalSEK })
		if limit > 0 && len(totals) > limit {
			totals = totals[:limit]
		}

		return jsonResult(totals)
	}
}

func apByCurrencyHandler(deps Deps) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		invoices, err := deps.SupplierLdg.UnpaidInvoices(ctx, deps.TenantID)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("fetch invoices: %v", err)), nil
		}

		byCurrency := analyst.GroupBy(invoices, func(i domain.SupplierInvoice) string { return i.Amount.Currency })
		type currencyGroup struct {
			Currency string  `json:"currency"`
			Count    int     `json:"count"`
			Total    float64 `json:"total"`
		}
		var groups []currencyGroup
		for ccy, invs := range byCurrency {
			var sum int64
			for _, inv := range invs {
				sum += inv.Amount.MinorUnits
			}
			groups = append(groups, currencyGroup{Currency: ccy, Count: len(invs), Total: float64(sum) / 100})
		}

		return jsonResult(groups)
	}
}

func apAgingHandler(deps Deps) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		invoices, err := deps.SupplierLdg.UnpaidInvoices(ctx, deps.TenantID)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("fetch invoices: %v", err)), nil
		}
		dueDates := make([]string, len(invoices))
		for i, inv := range invoices {
			dueDates[i] = inv.DueDate
		}
		report := analyst.AgingBuckets(dueDates, time.Now())
		return jsonResult(report)
	}
}

func apSupplierHistoryHandler(deps Deps) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		supplierNum := int(req.GetArgFloat("supplier_number", 0))
		if supplierNum == 0 {
			return mcp.NewToolResultError("supplier_number is required"), nil
		}

		supplier, err := deps.SupplierLdg.SupplierDetail(ctx, deps.TenantID, supplierNum)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("fetch supplier: %v", err)), nil
		}

		invoices, err := deps.SupplierLdg.UnpaidInvoices(ctx, deps.TenantID)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("fetch invoices: %v", err)), nil
		}

		var supplierInvoices []domain.SupplierInvoice
		for _, inv := range invoices {
			if inv.SupplierNumber == supplierNum {
				supplierInvoices = append(supplierInvoices, inv)
			}
		}

		return jsonResult(map[string]any{
			"supplier": supplier,
			"invoices": supplierInvoices,
		})
	}
}

func apInvoiceDetailHandler(deps Deps) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		invNum := int(req.GetArgFloat("invoice_number", 0))
		if invNum == 0 {
			return mcp.NewToolResultError("invoice_number is required"), nil
		}

		payments, err := deps.SupplierLdg.InvoicePayments(ctx, deps.TenantID, invNum)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("fetch payments: %v", err)), nil
		}

		return jsonResult(map[string]any{
			"invoice_number": invNum,
			"payments":       payments,
		})
	}
}

// jsonResult marshals v to JSON and returns an MCP text result.
func jsonResult(v any) (*mcp.CallToolResult, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("marshal result: %v", err)), nil
	}
	return mcp.NewToolResultText(string(b)), nil
}
```

- [ ] **Step 4: Create MCP entrypoint**

```go
// cmd/mcp/main.go
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
		log.Error("fortnox config required for MCP server", "err", err)
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
```

- [ ] **Step 5: Verify it compiles**

Run: `cd /Users/mathias/Documents/local-dev/AGENTS/cobalt-dingo && go build ./cmd/mcp/...`
Expected: no errors (may need `go mod tidy` first)

- [ ] **Step 6: Commit**

```bash
git add cmd/mcp/main.go internal/mcp/server.go internal/mcp/tools_ap.go go.mod go.sum
git commit -m "feat: MCP server scaffold with 7 AP tools"
```

---

### Task 13: AR, GL, and remaining MCP tools

**Files:**
- Create: `internal/mcp/tools_ar.go`
- Create: `internal/mcp/tools_gl.go`
- Create: `internal/mcp/tools_project.go`
- Create: `internal/mcp/tools_costcenter.go`
- Create: `internal/mcp/tools_asset.go`
- Modify: `internal/mcp/server.go` — uncomment registration calls

- [ ] **Step 1: Create AR tools**

Follow the same pattern as `tools_ap.go`. The AR tools mirror the AP tools but use `deps.CustomerLdg` and `domain.CustomerInvoice`. Tool names: `ar_summary`, `ar_overdue`, `ar_by_customer`, `ar_aging`, `ar_customer_history`, `ar_invoice_detail`, `ar_unpaid_report`.

Key difference: `ar_unpaid_report` includes customer contact info (email, phone) from `CustomerDetail` for each overdue customer.

Register all via `registerARTools(s *server.MCPServer, deps Deps)`.

- [ ] **Step 2: Create GL tools**

Tool names: `gl_chart_of_accounts`, `gl_account_balance`, `gl_account_activity`, `gl_vouchers`, `gl_voucher_detail`, `gl_predefined_accounts`, `gl_financial_years`.

Each tool calls the corresponding `deps.GeneralLdg` method. `gl_chart_of_accounts` and `gl_account_balance` take optional `year_id` (default to latest year from `FinancialYears`). `gl_account_activity` and `gl_vouchers` take `from_date` and `to_date` strings, parse them with `time.Parse("2006-01-02", ...)`.

Register via `registerGLTools(s *server.MCPServer, deps Deps)`.

- [ ] **Step 3: Create project, cost center, and asset tools**

`tools_project.go`: `project_list`, `project_transactions`, `project_profitability`. `project_profitability` fetches transactions and sums debit (cost) vs credit (revenue) rows.

`tools_costcenter.go`: `costcenter_list`, `costcenter_transactions`, `costcenter_analysis`. `costcenter_analysis` groups transactions by account number.

`tools_asset.go`: `asset_list`, `asset_detail`. Thin wrappers around the adapter.

Register via `registerProjectTools`, `registerCostCtrTools`, `registerAssetTools`.

- [ ] **Step 4: Uncomment registration in server.go**

Replace the commented lines in `internal/mcp/server.go` with actual calls:

```go
registerARTools(s, deps)
registerGLTools(s, deps)
registerProjectTools(s, deps)
registerCostCtrTools(s, deps)
registerAssetTools(s, deps)
```

- [ ] **Step 5: Verify it compiles**

Run: `cd /Users/mathias/Documents/local-dev/AGENTS/cobalt-dingo && go build ./cmd/mcp/...`
Expected: no errors

- [ ] **Step 6: Commit**

```bash
git add internal/mcp/tools_ar.go internal/mcp/tools_gl.go internal/mcp/tools_project.go internal/mcp/tools_costcenter.go internal/mcp/tools_asset.go internal/mcp/server.go
git commit -m "feat: add AR, GL, project, cost center, and asset MCP tools"
```

---

### Task 14: Analytics / BI tools

**Files:**
- Create: `internal/mcp/tools_analytics.go`
- Modify: `internal/mcp/server.go` — add `registerAnalyticsTools` call

- [ ] **Step 1: Implement analytics tools**

```go
// internal/mcp/tools_analytics.go
package mcp

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/mathiasb/cobalt-dingo/internal/analyst"
	"github.com/mathiasb/cobalt-dingo/internal/domain"
)

func registerAnalyticsTools(s *server.MCPServer, deps Deps) {
	s.AddTool(mcp.NewTool("cash_flow_forecast",
		mcp.WithDescription("Projected cash inflows (AR) and outflows (AP) for the next N days"),
		mcp.WithNumber("days_ahead", mcp.Description("Forecast horizon in days (default 30)")),
	), cashFlowForecastHandler(deps))

	s.AddTool(mcp.NewTool("expense_analysis",
		mcp.WithDescription("Costs grouped by BAS account category for a date range"),
		mcp.WithString("from_date", mcp.Required(), mcp.Description("Start date YYYY-MM-DD")),
		mcp.WithString("to_date", mcp.Required(), mcp.Description("End date YYYY-MM-DD")),
	), expenseAnalysisHandler(deps))

	s.AddTool(mcp.NewTool("period_comparison",
		mcp.WithDescription("Compare revenue, costs, and margin between two periods"),
		mcp.WithString("period1_from", mcp.Required(), mcp.Description("Period 1 start YYYY-MM-DD")),
		mcp.WithString("period1_to", mcp.Required(), mcp.Description("Period 1 end YYYY-MM-DD")),
		mcp.WithString("period2_from", mcp.Required(), mcp.Description("Period 2 start YYYY-MM-DD")),
		mcp.WithString("period2_to", mcp.Required(), mcp.Description("Period 2 end YYYY-MM-DD")),
	), periodComparisonHandler(deps))

	s.AddTool(mcp.NewTool("yearly_comparison",
		mcp.WithDescription("Year-over-year comparison of key financial metrics"),
		mcp.WithNumber("year1", mcp.Required(), mcp.Description("First financial year ID")),
		mcp.WithNumber("year2", mcp.Required(), mcp.Description("Second financial year ID")),
	), yearlyComparisonHandler(deps))

	s.AddTool(mcp.NewTool("gross_margin_trend",
		mcp.WithDescription("Monthly gross margin trend for the last N months"),
		mcp.WithNumber("months", mcp.Description("Number of months (default 12)")),
	), grossMarginTrendHandler(deps))

	s.AddTool(mcp.NewTool("top_customers",
		mcp.WithDescription("Highest-revenue customers for a date range"),
		mcp.WithString("from_date", mcp.Required(), mcp.Description("Start date YYYY-MM-DD")),
		mcp.WithString("to_date", mcp.Required(), mcp.Description("End date YYYY-MM-DD")),
		mcp.WithNumber("limit", mcp.Description("Max results (default 10)")),
	), topCustomersHandler(deps))

	s.AddTool(mcp.NewTool("top_suppliers",
		mcp.WithDescription("Highest-cost suppliers for a date range"),
		mcp.WithString("from_date", mcp.Required(), mcp.Description("Start date YYYY-MM-DD")),
		mcp.WithString("to_date", mcp.Required(), mcp.Description("End date YYYY-MM-DD")),
		mcp.WithNumber("limit", mcp.Description("Max results (default 10)")),
	), topSuppliersHandler(deps))

	s.AddTool(mcp.NewTool("sales_vs_purchases",
		mcp.WithDescription("Revenue vs cost trend over a date range"),
		mcp.WithString("from_date", mcp.Required(), mcp.Description("Start date YYYY-MM-DD")),
		mcp.WithString("to_date", mcp.Required(), mcp.Description("End date YYYY-MM-DD")),
	), salesVsPurchasesHandler(deps))

	s.AddTool(mcp.NewTool("company_info",
		mcp.WithDescription("Company profile: name, org number, address, fiscal year"),
	), companyInfoHandler(deps))
}

func cashFlowForecastHandler(deps Deps) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		daysAhead := int(req.GetArgFloat("days_ahead", 30))
		horizon := time.Now().AddDate(0, 0, daysAhead)

		// AP outflows
		apInvoices, err := deps.SupplierLdg.UnpaidInvoices(ctx, deps.TenantID)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("fetch AP: %v", err)), nil
		}
		var outflows float64
		var outCount int
		for _, inv := range apInvoices {
			d, _ := time.Parse("2006-01-02", inv.DueDate)
			if !d.IsZero() && !d.After(horizon) {
				outflows += inv.Amount.Float()
				outCount++
			}
		}

		// AR inflows
		arInvoices, err := deps.CustomerLdg.UnpaidInvoices(ctx, deps.TenantID)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("fetch AR: %v", err)), nil
		}
		var inflows float64
		var inCount int
		for _, inv := range arInvoices {
			d, _ := time.Parse("2006-01-02", inv.DueDate)
			if !d.IsZero() && !d.After(horizon) {
				inflows += inv.Balance.Float()
				inCount++
			}
		}

		return jsonResult(map[string]any{
			"horizon_days": daysAhead,
			"horizon_date": horizon.Format("2006-01-02"),
			"inflows":      map[string]any{"count": inCount, "total": inflows},
			"outflows":     map[string]any{"count": outCount, "total": outflows},
			"net_flow":     inflows - outflows,
		})
	}
}

func expenseAnalysisHandler(deps Deps) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		fromStr, _ := req.Params.Arguments["from_date"].(string)
		toStr, _ := req.Params.Arguments["to_date"].(string)
		from, _ := time.Parse("2006-01-02", fromStr)
		to, _ := time.Parse("2006-01-02", toStr)

		years, err := deps.GeneralLdg.FinancialYears(ctx, deps.TenantID)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("fetch years: %v", err)), nil
		}
		yearID := latestYearID(years)

		vouchers, err := deps.GeneralLdg.Vouchers(ctx, deps.TenantID, yearID, from, to)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("fetch vouchers: %v", err)), nil
		}

		// Group debit amounts by account (expense accounts are typically 4000-7999 in BAS)
		type acctTotal struct {
			Account int     `json:"account"`
			Total   float64 `json:"total"`
		}
		totals := map[int]int64{}
		for _, v := range vouchers {
			for _, r := range v.Rows {
				if r.Account >= 4000 && r.Account <= 7999 && r.Debit.MinorUnits > 0 {
					totals[r.Account] += r.Debit.MinorUnits
				}
			}
		}

		var result []acctTotal
		for acct, total := range totals {
			result = append(result, acctTotal{Account: acct, Total: float64(total) / 100})
		}
		sort.Slice(result, func(i, j int) bool { return result[i].Total > result[j].Total })

		return jsonResult(result)
	}
}

func periodComparisonHandler(deps Deps) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		p1From, _ := time.Parse("2006-01-02", req.Params.Arguments["period1_from"].(string))
		p1To, _ := time.Parse("2006-01-02", req.Params.Arguments["period1_to"].(string))
		p2From, _ := time.Parse("2006-01-02", req.Params.Arguments["period2_from"].(string))
		p2To, _ := time.Parse("2006-01-02", req.Params.Arguments["period2_to"].(string))

		years, _ := deps.GeneralLdg.FinancialYears(ctx, deps.TenantID)
		yearID := latestYearID(years)

		p1Metrics, err := periodMetrics(ctx, deps, yearID, p1From, p1To)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("period 1: %v", err)), nil
		}
		p2Metrics, err := periodMetrics(ctx, deps, yearID, p2From, p2To)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("period 2: %v", err)), nil
		}

		return jsonResult(map[string]any{
			"period_1": p1Metrics,
			"period_2": p2Metrics,
		})
	}
}

func yearlyComparisonHandler(deps Deps) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		y1 := int(req.GetArgFloat("year1", 0))
		y2 := int(req.GetArgFloat("year2", 0))

		bal1, err := deps.GeneralLdg.AccountBalances(ctx, deps.TenantID, y1, 1000, 9999)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("year %d: %v", y1, err)), nil
		}
		bal2, err := deps.GeneralLdg.AccountBalances(ctx, deps.TenantID, y2, 1000, 9999)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("year %d: %v", y2, err)), nil
		}

		return jsonResult(map[string]any{
			"year_1": map[string]any{"year_id": y1, "accounts": bal1},
			"year_2": map[string]any{"year_id": y2, "accounts": bal2},
		})
	}
}

func grossMarginTrendHandler(deps Deps) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		months := int(req.GetArgFloat("months", 12))
		now := time.Now()

		years, _ := deps.GeneralLdg.FinancialYears(ctx, deps.TenantID)
		yearID := latestYearID(years)

		type monthPoint struct {
			Month   string  `json:"month"`
			Revenue float64 `json:"revenue"`
			COGS    float64 `json:"cogs"`
			Margin  float64 `json:"margin_pct"`
		}
		var points []monthPoint
		for i := months - 1; i >= 0; i-- {
			mStart := time.Date(now.Year(), now.Month()-time.Month(i), 1, 0, 0, 0, 0, time.UTC)
			mEnd := mStart.AddDate(0, 1, -1)
			m, _ := periodMetrics(ctx, deps, yearID, mStart, mEnd)
			pct := 0.0
			if m.Revenue > 0 {
				pct = (m.Revenue - m.COGS) / m.Revenue * 100
			}
			points = append(points, monthPoint{
				Month:   mStart.Format("2006-01"),
				Revenue: m.Revenue,
				COGS:    m.COGS,
				Margin:  pct,
			})
		}

		return jsonResult(points)
	}
}

func topCustomersHandler(deps Deps) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		limit := int(req.GetArgFloat("limit", 10))
		invoices, err := deps.CustomerLdg.UnpaidInvoices(ctx, deps.TenantID)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("fetch AR: %v", err)), nil
		}

		type custTotal struct {
			CustomerNumber int     `json:"customer_number"`
			CustomerName   string  `json:"customer_name"`
			Total          float64 `json:"total"`
			Count          int     `json:"count"`
		}
		byCust := analyst.GroupBy(invoices, func(i domain.CustomerInvoice) int { return i.CustomerNumber })
		var totals []custTotal
		for _, invs := range byCust {
			var sum int64
			for _, inv := range invs {
				sum += inv.Amount.MinorUnits
			}
			totals = append(totals, custTotal{
				CustomerNumber: invs[0].CustomerNumber,
				CustomerName:   invs[0].CustomerName,
				Total:          float64(sum) / 100,
				Count:          len(invs),
			})
		}
		sort.Slice(totals, func(i, j int) bool { return totals[i].Total > totals[j].Total })
		if limit > 0 && len(totals) > limit {
			totals = totals[:limit]
		}

		return jsonResult(totals)
	}
}

func topSuppliersHandler(deps Deps) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		limit := int(req.GetArgFloat("limit", 10))
		invoices, err := deps.SupplierLdg.UnpaidInvoices(ctx, deps.TenantID)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("fetch AP: %v", err)), nil
		}

		type suppTotal struct {
			SupplierNumber int     `json:"supplier_number"`
			SupplierName   string  `json:"supplier_name"`
			Total          float64 `json:"total"`
			Count          int     `json:"count"`
		}
		bySupp := analyst.GroupBy(invoices, func(i domain.SupplierInvoice) int { return i.SupplierNumber })
		var totals []suppTotal
		for _, invs := range bySupp {
			var sum int64
			for _, inv := range invs {
				sum += inv.Amount.MinorUnits
			}
			totals = append(totals, suppTotal{
				SupplierNumber: invs[0].SupplierNumber,
				SupplierName:   invs[0].SupplierName,
				Total:          float64(sum) / 100,
				Count:          len(invs),
			})
		}
		sort.Slice(totals, func(i, j int) bool { return totals[i].Total > totals[j].Total })
		if limit > 0 && len(totals) > limit {
			totals = totals[:limit]
		}

		return jsonResult(totals)
	}
}

func salesVsPurchasesHandler(deps Deps) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		fromStr, _ := req.Params.Arguments["from_date"].(string)
		toStr, _ := req.Params.Arguments["to_date"].(string)
		from, _ := time.Parse("2006-01-02", fromStr)
		to, _ := time.Parse("2006-01-02", toStr)

		years, _ := deps.GeneralLdg.FinancialYears(ctx, deps.TenantID)
		yearID := latestYearID(years)

		m, err := periodMetrics(ctx, deps, yearID, from, to)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("metrics: %v", err)), nil
		}

		return jsonResult(map[string]any{
			"from":      fromStr,
			"to":        toStr,
			"revenue":   m.Revenue,
			"purchases": m.COGS,
			"net":       m.Revenue - m.COGS,
		})
	}
}

func companyInfoHandler(deps Deps) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		info, err := deps.CompanyInf.Info(ctx, deps.TenantID)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("company info: %v", err)), nil
		}
		return jsonResult(info)
	}
}

// --- helpers ---

type metrics struct {
	Revenue float64 `json:"revenue"`
	COGS    float64 `json:"cogs"`
}

func periodMetrics(ctx context.Context, deps Deps, yearID int, from, to time.Time) (metrics, error) {
	vouchers, err := deps.GeneralLdg.Vouchers(ctx, deps.TenantID, yearID, from, to)
	if err != nil {
		return metrics{}, err
	}
	var m metrics
	for _, v := range vouchers {
		for _, r := range v.Rows {
			// BAS: 3000-3999 = revenue (credit side), 4000-4999 = COGS (debit side)
			if r.Account >= 3000 && r.Account <= 3999 {
				m.Revenue += r.Credit.Float()
			}
			if r.Account >= 4000 && r.Account <= 4999 {
				m.COGS += r.Debit.Float()
			}
		}
	}
	return m, nil
}

func latestYearID(years []domain.FinancialYear) int {
	if len(years) == 0 {
		return 1
	}
	latest := years[0]
	for _, y := range years[1:] {
		if y.To.After(latest.To) {
			latest = y
		}
	}
	return latest.ID
}
```

- [ ] **Step 2: Register in server.go**

Add `registerAnalyticsTools(s, deps)` to `NewServer()`.

- [ ] **Step 3: Verify it compiles**

Run: `cd /Users/mathias/Documents/local-dev/AGENTS/cobalt-dingo && go build ./cmd/mcp/...`
Expected: no errors

- [ ] **Step 4: Commit**

```bash
git add internal/mcp/tools_analytics.go internal/mcp/server.go
git commit -m "feat: add 9 analytics/BI MCP tools"
```

---

## Phase 4: HTMX Chat UI

### Task 15: Chat handler with Claude API and SSE

**Files:**
- Modify: `internal/config/config.go` — add Claude API config
- Create: `internal/ui/chat.go`
- Modify: `cmd/server/main.go` — wire chat handler

- [ ] **Step 1: Add Claude config**

Add to `internal/config/config.go`:

```go
// Claude holds Claude API configuration for the chat interface.
type Claude struct {
	APIKey string
	Model  string // default: "claude-sonnet-4-6"
}

// LoadClaude reads Claude config from environment variables.
func LoadClaude() Claude {
	model := os.Getenv("CLAUDE_MODEL")
	if model == "" {
		model = "claude-sonnet-4-6"
	}
	return Claude{
		APIKey: os.Getenv("ANTHROPIC_API_KEY"),
		Model:  model,
	}
}
```

- [ ] **Step 2: Create chat handler**

The chat handler receives a user message, calls the Claude API with 38 tool schemas, executes tool calls locally, streams the response via SSE. This is the largest single file — ~200 lines including the SSE streaming and tool dispatch loop.

Create `internal/ui/chat.go` with:
- `ChatHandler` struct holding `mcpserver.Deps`, `config.Claude`, and `*slog.Logger`
- `ServeHTTP` method that handles `GET /chat` (serve chat page) and `POST /chat` (process message)
- The POST handler: parse JSON body `{messages: [...], message: "user text"}`, build Claude API request with tool schemas, execute tool-use loop, stream text deltas via SSE
- Use `net/http` for the Anthropic API call (avoid adding the Anthropic SDK as a dependency — it's just a POST to `https://api.anthropic.com/v1/messages`)

- [ ] **Step 3: Wire in main.go**

Add to `cmd/server/main.go`:

```go
claudeCfg := config.LoadClaude()
if claudeCfg.APIKey != "" {
    chatHandler := ui.NewChatHandler(mcpDeps, claudeCfg, log)
    mux.HandleFunc("GET /chat", chatHandler.PageHandler)
    mux.HandleFunc("POST /chat", chatHandler.MessageHandler)
    log.Info("chat enabled", "model", claudeCfg.Model)
}
```

Where `mcpDeps` is built from the same adapters already wired.

- [ ] **Step 4: Verify it compiles**

Run: `cd /Users/mathias/Documents/local-dev/AGENTS/cobalt-dingo && go build ./cmd/server/...`
Expected: no errors

- [ ] **Step 5: Commit**

```bash
git add internal/config/config.go internal/ui/chat.go cmd/server/main.go
git commit -m "feat: add chat handler with Claude API and SSE streaming"
```

---

### Task 16: Chat templ template

**Files:**
- Create: `internal/ui/chat.templ`
- Modify: `internal/ui/layout.templ` — add Chat nav link

- [ ] **Step 1: Create chat template**

```templ
// internal/ui/chat.templ
package ui

templ ChatPage() {
	@Layout("Chat") {
		<div id="chat-container" class="chat-container">
			<div id="messages" class="chat-messages"></div>
			<form id="chat-form" class="chat-input-form">
				<input type="text" id="chat-input" name="message" placeholder="Ask about your finances..." autocomplete="off" />
				<button type="submit">Send</button>
			</form>
		</div>
		<script src="https://cdn.jsdelivr.net/npm/chart.js@4/dist/chart.umd.min.js"></script>
		<script>
		// Chat JS: handles form submission, SSE streaming, markdown tables, Chart.js rendering
		const messages = document.getElementById('messages');
		const form = document.getElementById('chat-form');
		const input = document.getElementById('chat-input');
		let history = [];

		form.addEventListener('submit', async (e) => {
			e.preventDefault();
			const text = input.value.trim();
			if (!text) return;
			input.value = '';

			appendMessage('user', text);
			history.push({role: 'user', content: text});

			const assistantDiv = appendMessage('assistant', '');
			assistantDiv.innerHTML = '<em>Thinking...</em>';

			try {
				const resp = await fetch('/chat', {
					method: 'POST',
					headers: {'Content-Type': 'application/json'},
					body: JSON.stringify({messages: history, message: text}),
				});

				assistantDiv.innerHTML = '';
				const reader = resp.body.getReader();
				const decoder = new TextDecoder();
				let fullText = '';

				while (true) {
					const {done, value} = await reader.read();
					if (done) break;
					const chunk = decoder.decode(value, {stream: true});
					const lines = chunk.split('\n');
					for (const line of lines) {
						if (line.startsWith('data: ')) {
							const data = line.slice(6);
							if (data === '[DONE]') continue;
							fullText += data;
							assistantDiv.innerHTML = fullText;
						}
					}
				}

				history.push({role: 'assistant', content: fullText});
				renderCharts(assistantDiv);
			} catch (err) {
				assistantDiv.innerHTML = '<em>Error: ' + err.message + '</em>';
			}
			messages.scrollTop = messages.scrollHeight;
		});

		function appendMessage(role, text) {
			const div = document.createElement('div');
			div.className = 'chat-message chat-' + role;
			div.innerHTML = text;
			messages.appendChild(div);
			messages.scrollTop = messages.scrollHeight;
			return div;
		}

		function renderCharts(container) {
			container.querySelectorAll('[data-chart]').forEach(el => {
				try {
					const spec = JSON.parse(el.dataset.chart);
					const canvas = document.createElement('canvas');
					canvas.style.maxHeight = '300px';
					el.appendChild(canvas);
					new Chart(canvas, {
						type: spec.type || 'bar',
						data: {labels: spec.labels, datasets: [{label: spec.label || '', data: spec.values, backgroundColor: spec.colors}]},
						options: {responsive: true, maintainAspectRatio: false},
					});
				} catch(e) { console.error('chart render:', e); }
			});
		}
		</script>
		<style>
		.chat-container { display: flex; flex-direction: column; height: calc(100vh - 60px); max-width: 800px; margin: 0 auto; }
		.chat-messages { flex: 1; overflow-y: auto; padding: 1rem; }
		.chat-message { margin: 0.5rem 0; padding: 0.75rem 1rem; border-radius: 8px; max-width: 85%; }
		.chat-user { background: var(--accent, #e8f5e9); margin-left: auto; text-align: right; }
		.chat-assistant { background: var(--surface, #f5f5f5); }
		.chat-input-form { display: flex; gap: 0.5rem; padding: 1rem; border-top: 1px solid var(--border, #ddd); }
		.chat-input-form input { flex: 1; padding: 0.5rem; border: 1px solid var(--border, #ddd); border-radius: 4px; }
		.chat-input-form button { padding: 0.5rem 1.5rem; }
		</style>
	}
}
```

- [ ] **Step 2: Add Chat link to navigation**

In `internal/ui/layout.templ`, add a Chat link to the navbar next to the existing "AP automation" text.

- [ ] **Step 3: Generate templ code**

Run: `cd /Users/mathias/Documents/local-dev/AGENTS/cobalt-dingo && templ generate ./...`
Expected: generates `chat_templ.go` and updated `layout_templ.go`

- [ ] **Step 4: Verify it compiles and renders**

Run: `cd /Users/mathias/Documents/local-dev/AGENTS/cobalt-dingo && go build ./cmd/server/...`
Expected: no errors

- [ ] **Step 5: Commit**

```bash
git add internal/ui/chat.templ internal/ui/chat_templ.go internal/ui/layout.templ internal/ui/layout_templ.go
git commit -m "feat: add chat UI template with Chart.js support"
```

---

## Phase 5: Integration and Polish

### Task 17: MCP Claude config and smoke test

**Files:**
- Modify: `.mcp.json` (already untracked, local config)

- [ ] **Step 1: Create/update local MCP config**

```json
{
  "mcpServers": {
    "cobalt-dingo": {
      "command": "go",
      "args": ["run", "./cmd/mcp"],
      "cwd": "/Users/mathias/Documents/local-dev/AGENTS/cobalt-dingo",
      "env": {
        "FORTNOX_CLIENT_ID": "${FORTNOX_CLIENT_ID}",
        "FORTNOX_CLIENT_SECRET": "${FORTNOX_CLIENT_SECRET}",
        "FORTNOX_REDIRECT_URI": "http://localhost:8080/callback",
        "FORTNOX_ENV": "sandbox"
      }
    }
  }
}
```

- [ ] **Step 2: Smoke test — verify MCP server starts**

Run: `cd /Users/mathias/Documents/local-dev/AGENTS/cobalt-dingo && source .env && go run ./cmd/mcp 2>&1 &; sleep 2; kill %1`
Expected: server starts without errors, logs "MCP server starting"

- [ ] **Step 3: Update Taskfile with MCP commands**

Add to `Taskfile.yml`:

```yaml
  mcp:
    desc: Start MCP server (stdio)
    cmds:
      - source .env && go run ./cmd/mcp

  mcp:build:
    desc: Build MCP server binary
    cmds:
      - go build -trimpath -ldflags="-s -w" -o bin/cobalt-dingo-mcp ./cmd/mcp
```

- [ ] **Step 4: Commit**

```bash
git add Taskfile.yml
git commit -m "chore: add MCP server task commands"
```

---

### Task 18: Run full test suite and fix issues

- [ ] **Step 1: Run all unit tests**

Run: `cd /Users/mathias/Documents/local-dev/AGENTS/cobalt-dingo && go test -race -count=1 $(go list ./... | grep -v /acceptance) -v`
Expected: all tests pass

- [ ] **Step 2: Run vet and lint**

Run: `cd /Users/mathias/Documents/local-dev/AGENTS/cobalt-dingo && go vet ./...`
Run: `cd /Users/mathias/Documents/local-dev/AGENTS/cobalt-dingo && golangci-lint run ./...`
Expected: no errors. Fix any issues found.

- [ ] **Step 3: Fix any compilation or test failures**

Address each failure individually. Common issues:
- Missing imports in test files (e.g., `time` package in test helpers)
- Unused imports
- `GetArgFloat` method may have a different signature in mcp-go — check the actual API

- [ ] **Step 4: Commit fixes**

```bash
git add -A
git commit -m "fix: resolve test and lint issues"
```

---

### Task 19: Manual integration test with Claude

- [ ] **Step 1: Start the web server**

Run: `cd /Users/mathias/Documents/local-dev/AGENTS/cobalt-dingo && source .env && go run ./cmd/server`

- [ ] **Step 2: Test the chat UI**

Open `http://localhost:8080/chat` in a browser. Try:
- "What's my AP summary?"
- "Which invoices are overdue?"
- "Show me AP aging"

Verify:
- Messages stream via SSE
- Tables render correctly
- No JS console errors

- [ ] **Step 3: Test MCP tools from Claude Code**

With the MCP server configured, ask Claude:
- "Use the ap_summary tool"
- "Show me overdue AR invoices"
- "What does my chart of accounts look like?"

Verify: tools execute and return structured JSON.

- [ ] **Step 4: Document any issues found**

If endpoints return unexpected data or errors, note them for follow-up.

- [ ] **Step 5: Final commit**

```bash
git add -A
git commit -m "feat: financial command center v0.5.0 — 38 MCP tools, chat UI"
```
