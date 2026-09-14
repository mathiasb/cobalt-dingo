package domain

import (
	"context"
	"time"
)

// SupplierLedger provides access to the accounts payable sub-ledger.
type SupplierLedger interface {
	// UnpaidInvoices answers "what is unpaid", which is NOT "what does the
	// company owe": Fortnox's `unpaid` filter excludes unbooked invoices.
	// StateCounts is what makes an empty result here interpretable.
	UnpaidInvoices(ctx context.Context, tenantID TenantID) ([]SupplierInvoice, error)

	// StateCounts returns the invoice count under every documented status
	// filter, so a zero elsewhere can be told apart from an unmeasured one.
	StateCounts(ctx context.Context, tenantID TenantID) ([]InvoiceStateCount, error)

	// UnbookedInvoices returns invoices registered but not booked. They are
	// obligations that appear in neither the unpaid view nor the general
	// ledger, so a count of them is not enough — the report has to be able to
	// name them (#91).
	UnbookedInvoices(ctx context.Context, tenantID TenantID) ([]SupplierInvoice, error)
	InvoicePayments(ctx context.Context, tenantID TenantID, invoiceNumber int) ([]SupplierPayment, error)
	SupplierDetail(ctx context.Context, tenantID TenantID, supplierNumber int) (Supplier, error)
}

// CustomerLedger provides access to the accounts receivable sub-ledger.
type CustomerLedger interface {
	UnpaidInvoices(ctx context.Context, tenantID TenantID) ([]CustomerInvoice, error)

	// StateCounts returns the invoice count under every documented status
	// filter — see SupplierLedger.StateCounts.
	StateCounts(ctx context.Context, tenantID TenantID) ([]InvoiceStateCount, error)

	// UnbookedInvoices returns invoices registered but not booked — money owed
	// TO the company that no ledger balance reflects.
	UnbookedInvoices(ctx context.Context, tenantID TenantID) ([]CustomerInvoice, error)
	InvoicePayments(ctx context.Context, tenantID TenantID, invoiceNumber int) ([]CustomerPayment, error)
	CustomerDetail(ctx context.Context, tenantID TenantID, customerNumber int) (Customer, error)
}

// GeneralLedger provides access to the general ledger and chart of accounts.
type GeneralLedger interface {
	ChartOfAccounts(ctx context.Context, tenantID TenantID, yearID int) ([]Account, error)
	AccountBalances(ctx context.Context, tenantID TenantID, yearID int, fromAcct, toAcct int) ([]AccountBalance, error)
	AccountActivity(ctx context.Context, tenantID TenantID, yearID int, acctNum int, from, to time.Time) ([]VoucherRow, error)
	Vouchers(ctx context.Context, tenantID TenantID, yearID int, from, to time.Time) ([]Voucher, error)
	// VoucherDetail requires yearID: voucher numbers restart per series each
	// financial year, so (series, number) alone names one voucher per year the
	// company has traded. The ambiguity was not theoretical — see
	// fortnox.Client.GetVoucher.
	VoucherDetail(ctx context.Context, tenantID TenantID, yearID int, series string, number int) (Voucher, error)
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
