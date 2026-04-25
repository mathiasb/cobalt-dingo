package fortnox

import (
	"context"
	"fmt"
	"time"

	"github.com/mathiasb/cobalt-dingo/internal/domain"
	rawfortnox "github.com/mathiasb/cobalt-dingo/internal/fortnox"
)

// GeneralLedgerAdapter implements domain.GeneralLedger using the Fortnox REST API.
type GeneralLedgerAdapter struct {
	baseURL string
	tokens  domain.TokenStore
}

// NewGeneralLedgerAdapter returns a GeneralLedgerAdapter pointed at baseURL and
// backed by the given token store.
func NewGeneralLedgerAdapter(baseURL string, tokens domain.TokenStore) *GeneralLedgerAdapter {
	return &GeneralLedgerAdapter{baseURL: baseURL, tokens: tokens}
}

// client loads the tenant's access token and returns a ready-to-use raw Fortnox client.
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
		return nil, fmt.Errorf("general ledger chart of accounts: %w", err)
	}
	rows, err := c.ListAccounts(yearID)
	if err != nil {
		return nil, fmt.Errorf("general ledger chart of accounts: %w", err)
	}

	accounts := make([]domain.Account, len(rows))
	for i, r := range rows {
		accounts[i] = domain.Account{
			Number:      r.Number,
			Description: r.Description,
			SRU:         r.SRU,
			Active:      r.Active,
			Year:        yearID,
			BalanceBF:   domain.MoneyFromFloat(r.BalanceBroughtForward, "SEK"),
			BalanceCF:   domain.MoneyFromFloat(r.BalanceCarriedForward, "SEK"),
		}
	}
	return accounts, nil
}

// AccountBalances implements domain.GeneralLedger. It fetches the chart of
// accounts and returns non-zero balances for accounts in [fromAcct, toAcct].
func (a *GeneralLedgerAdapter) AccountBalances(ctx context.Context, tenantID domain.TenantID, yearID int, fromAcct, toAcct int) ([]domain.AccountBalance, error) {
	accounts, err := a.ChartOfAccounts(ctx, tenantID, yearID)
	if err != nil {
		return nil, fmt.Errorf("general ledger account balances: %w", err)
	}

	var result []domain.AccountBalance
	for _, acc := range accounts {
		if acc.Number < fromAcct || acc.Number > toAcct {
			continue
		}
		if acc.BalanceCF.MinorUnits == 0 {
			continue
		}
		result = append(result, domain.AccountBalance{
			AccountNumber: acc.Number,
			Balance:       acc.BalanceCF,
		})
	}
	return result, nil
}

// AccountActivity implements domain.GeneralLedger. It fetches all vouchers for
// the year and returns rows matching acctNum within the [from, to] date range.
func (a *GeneralLedgerAdapter) AccountActivity(ctx context.Context, tenantID domain.TenantID, yearID int, acctNum int, from, to time.Time) ([]domain.VoucherRow, error) {
	vouchers, err := a.Vouchers(ctx, tenantID, yearID, from, to)
	if err != nil {
		return nil, fmt.Errorf("general ledger account activity: %w", err)
	}

	var rows []domain.VoucherRow
	for _, v := range vouchers {
		for _, row := range v.Rows {
			if row.Account == acctNum {
				rows = append(rows, row)
			}
		}
	}
	return rows, nil
}

// Vouchers implements domain.GeneralLedger. It fetches all vouchers for the
// financial year and filters to those with TransactionDate in [from, to].
func (a *GeneralLedgerAdapter) Vouchers(ctx context.Context, tenantID domain.TenantID, yearID int, from, to time.Time) ([]domain.Voucher, error) {
	c, err := a.client(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("general ledger vouchers: %w", err)
	}
	rawVouchers, err := c.ListVouchers(yearID)
	if err != nil {
		return nil, fmt.Errorf("general ledger vouchers: %w", err)
	}

	var result []domain.Voucher
	for _, rv := range rawVouchers {
		date, err := time.Parse("2006-01-02", rv.TransactionDate)
		if err != nil {
			return nil, fmt.Errorf("general ledger: parse voucher date %q: %w", rv.TransactionDate, err)
		}
		if date.Before(from) || date.After(to) {
			continue
		}
		result = append(result, convertVoucher(rv))
	}
	return result, nil
}

// VoucherDetail implements domain.GeneralLedger.
func (a *GeneralLedgerAdapter) VoucherDetail(ctx context.Context, tenantID domain.TenantID, series string, number int) (domain.Voucher, error) {
	c, err := a.client(ctx, tenantID)
	if err != nil {
		return domain.Voucher{}, fmt.Errorf("general ledger voucher detail: %w", err)
	}
	rv, err := c.GetVoucher(series, number)
	if err != nil {
		return domain.Voucher{}, fmt.Errorf("general ledger voucher detail: %w", err)
	}
	return convertVoucher(rv), nil
}

// FinancialYears implements domain.GeneralLedger.
func (a *GeneralLedgerAdapter) FinancialYears(ctx context.Context, tenantID domain.TenantID) ([]domain.FinancialYear, error) {
	c, err := a.client(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("general ledger financial years: %w", err)
	}
	rows, err := c.ListFinancialYears()
	if err != nil {
		return nil, fmt.Errorf("general ledger financial years: %w", err)
	}

	years := make([]domain.FinancialYear, len(rows))
	for i, r := range rows {
		from, err := time.Parse("2006-01-02", r.FromDate)
		if err != nil {
			return nil, fmt.Errorf("general ledger: parse from date %q: %w", r.FromDate, err)
		}
		to, err := time.Parse("2006-01-02", r.ToDate)
		if err != nil {
			return nil, fmt.Errorf("general ledger: parse to date %q: %w", r.ToDate, err)
		}
		years[i] = domain.FinancialYear{
			ID:   r.ID,
			From: from,
			To:   to,
		}
	}
	return years, nil
}

// PredefinedAccounts implements domain.GeneralLedger.
func (a *GeneralLedgerAdapter) PredefinedAccounts(ctx context.Context, tenantID domain.TenantID) ([]domain.PredefinedAccount, error) {
	c, err := a.client(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("general ledger predefined accounts: %w", err)
	}
	rows, err := c.ListPredefinedAccounts()
	if err != nil {
		return nil, fmt.Errorf("general ledger predefined accounts: %w", err)
	}

	result := make([]domain.PredefinedAccount, len(rows))
	for i, r := range rows {
		result[i] = domain.PredefinedAccount{
			Name:    r.Name,
			Account: r.Account,
		}
	}
	return result, nil
}

// convertVoucher converts a raw Fortnox VoucherJSON to a domain.Voucher.
func convertVoucher(rv rawfortnox.VoucherJSON) domain.Voucher {
	rows := make([]domain.VoucherRow, len(rv.VoucherRows))
	for i, r := range rv.VoucherRows {
		rows[i] = domain.VoucherRow{
			Account:     r.Account,
			Debit:       domain.MoneyFromFloat(r.Debit, "SEK"),
			Credit:      domain.MoneyFromFloat(r.Credit, "SEK"),
			Description: r.TransactionInformation,
			CostCenter:  r.CostCenter,
			Project:     r.Project,
		}
	}
	return domain.Voucher{
		Series:          rv.VoucherSeries,
		Number:          rv.VoucherNumber,
		Description:     rv.Description,
		TransactionDate: rv.TransactionDate,
		Year:            rv.Year,
		Rows:            rows,
	}
}
