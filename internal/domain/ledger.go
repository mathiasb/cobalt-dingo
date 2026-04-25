package domain

import "time"

// CustomerInvoice is a customer-side invoice from the ERP.
type CustomerInvoice struct {
	InvoiceNumber  int
	CustomerNumber int
	CustomerName   string
	Amount         Money
	Balance        Money
	DueDate        string
	InvoiceDate    string
	Booked         bool
	Cancelled      bool
	Sent           bool
}

// SupplierPayment is a payment made against a supplier invoice.
type SupplierPayment struct {
	PaymentNumber int
	InvoiceNumber int
	Amount        Money
	CurrencyRate  float64
	PaymentDate   string
	Booked        bool
}

// CustomerPayment is a payment received against a customer invoice.
type CustomerPayment struct {
	PaymentNumber int
	InvoiceNumber int
	Amount        Money
	PaymentDate   string
	Booked        bool
}

// Supplier is supplier master data.
type Supplier struct {
	SupplierNumber int
	Name           string
	Email          string
	Phone          string
	IBAN           string
	BIC            string
	Active         bool
}

// Customer is customer master data.
type Customer struct {
	CustomerNumber int
	Name           string
	Email          string
	Phone          string
	Active         bool
}

// Account is a GL account from the chart of accounts.
type Account struct {
	Number      int
	Description string
	SRU         int
	Active      bool
	Year        int
	BalanceBF   Money
	BalanceCF   Money
}

// AccountBalance is the period balance for an account.
type AccountBalance struct {
	AccountNumber int
	Period        string
	Balance       Money
}

// VoucherRow is a single line in a journal entry.
type VoucherRow struct {
	Account     int
	Debit       Money
	Credit      Money
	Description string
	CostCenter  string
	Project     string
}

// Voucher is a complete journal entry.
type Voucher struct {
	Series          string
	Number          int
	Description     string
	TransactionDate string
	Year            int
	Rows            []VoucherRow
}

// FinancialYear holds fiscal year boundaries.
type FinancialYear struct {
	ID   int
	From time.Time
	To   time.Time
}

// PredefinedAccount maps a system role name to a GL account number.
type PredefinedAccount struct {
	Name    string
	Account int
}

// Project is a project for cost and revenue tracking.
type Project struct {
	Number      string
	Description string
	Status      string
	StartDate   string
	EndDate     string
}

// CostCenter is a cost center.
type CostCenter struct {
	Code        string
	Description string
	Active      bool
}

// Asset is a fixed asset.
type Asset struct {
	ID                  int
	Number              string
	Description         string
	AcquisitionDate     string
	AcquisitionValue    Money
	DepreciationMethod  string
	DepreciationPercent float64
	BookValue           Money
	AccumulatedDepr     Money
}

// Company holds the company profile.
type Company struct {
	Name         string
	OrgNumber    string
	Address      string
	City         string
	ZipCode      string
	Country      string
	Email        string
	Phone        string
	VisitAddress string
	VisitCity    string
	VisitZipCode string
}
