// Package api provides a client for the Fortnox REST API with OAuth2,
// automatic token refresh, retry with exponential backoff, and audit logging.
//
// All write operations (POST/PUT) must only be called after explicit user
// confirmation – they are never invoked automatically by the scheduler.
package api

import (
	"context"
	"time"

	"github.com/mathiasb/coo-agent/internal/audit"
	"github.com/mathiasb/coo-agent/internal/auth"
	"github.com/mathiasb/coo-agent/internal/validator"
)

// FortnoxReader is the read-only subset of the Fortnox API used by
// the scheduler and reporting agent. It can be easily stubbed in tests.
type FortnoxReader interface {
	ListInvoices(ctx context.Context, filter string) ([]Invoice, error)
	ListVouchers(ctx context.Context, from, to time.Time) ([]Voucher, error)
	ListAccounts(ctx context.Context) ([]validator.Account, error)
	ListSupplierInvoices(ctx context.Context, filter string) ([]SupplierInvoice, error)
	ListAssets(ctx context.Context) ([]Asset, error)
	GetCompanyInfo(ctx context.Context) (*CompanyInfo, error)
}

// FortnoxWriter is the write subset used by the bookkeeper agent.
// All methods require prior user confirmation.
type FortnoxWriter interface {
	CreateVoucher(ctx context.Context, v Voucher) (*Voucher, error)
}

// FortnoxClient combines read and write access to the Fortnox API.
type FortnoxClient interface {
	FortnoxReader
	FortnoxWriter
}

// Config holds client configuration. All dependencies are injected as
// interfaces so the client is testable without a real Fortnox connection.
type Config struct {
	ClientID        string
	ClientSecret    string
	RedirectURI     string
	Sandbox         bool
	BaseURL         string // override for tests; defaults to production URL
	TokenRefreshURL string // override for tests; defaults to Fortnox OAuth endpoint
	TokenStore      auth.TokenStore
	AuditLog        audit.Logger
}

// Invoice represents a Fortnox customer invoice.
type Invoice struct {
	DocumentNumber string  `json:"DocumentNumber"`
	CustomerName   string  `json:"CustomerName"`
	InvoiceDate    string  `json:"InvoiceDate"`
	DueDate        string  `json:"DueDate"`
	Total          float64 `json:"Total"`
	Balance        float64 `json:"Balance"`
	Currency       string  `json:"Currency"`
	Sent           bool    `json:"Sent"`
	Booked         bool    `json:"Booked"`
	Cancelled      bool    `json:"Cancelled"`
}

// VoucherRow is a single debit/credit line in a voucher.
type VoucherRow struct {
	Account     int     `json:"Account"`
	Debit       float64 `json:"Debit"`
	Credit      float64 `json:"Credit"`
	Description string  `json:"TransactionInformation,omitempty"`
}

// Voucher represents a Fortnox accounting voucher.
type Voucher struct {
	VoucherSeries string       `json:"VoucherSeries,omitempty"`
	VoucherNumber int          `json:"VoucherNumber,omitempty"`
	Description   string       `json:"Description"`
	VoucherDate   string       `json:"VoucherDate"`
	Year          int          `json:"Year,omitempty"`
	Rows          []VoucherRow `json:"VoucherRows"`
}

// SupplierInvoice represents a Fortnox supplier invoice (leverantörsfaktura).
type SupplierInvoice struct {
	GivenNumber  string `json:"GivenNumber"`
	SupplierName string `json:"SupplierName"`
	InvoiceDate  string `json:"InvoiceDate"`
	DueDate      string `json:"DueDate"`
	Total        string `json:"Total"`
	Balance      string `json:"Balance"`
	Booked       bool   `json:"Booked"`
	Cancelled    bool   `json:"Cancelled"`
	OCR          string `json:"OCR"`
}

// Asset represents a Fortnox fixed asset (anläggningstillgång).
type Asset struct {
	Number           string `json:"Number"`
	Description      string `json:"Description"`
	Status           string `json:"Status"`
	AcquisitionDate  string `json:"AcquisitionDate"`
	AcquisitionValue int    `json:"AcquisitionValue"`
	DepreciatedTo    string `json:"DepreciatedTo"`
	Group            string `json:"Group"`
}

// CompanyInfo holds basic company information from Fortnox.
type CompanyInfo struct {
	CompanyName        string `json:"CompanyName"`
	OrganizationNumber string `json:"OrganizationNumber"`
	Address            string `json:"Address"`
	ZipCode            string `json:"ZipCode"`
	City               string `json:"City"`
	Country            string `json:"Country"`
}
