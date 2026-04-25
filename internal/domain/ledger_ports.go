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
