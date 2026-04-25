package fortnox

import (
	"context"
	"errors"
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

// AccountBalances implements domain.GeneralLedger. Not yet implemented.
func (a *GeneralLedgerAdapter) AccountBalances(_ context.Context, _ domain.TenantID, _ int, _, _ int) ([]domain.AccountBalance, error) {
	return nil, errors.New("general ledger: AccountBalances not implemented")
}

// AccountActivity implements domain.GeneralLedger. Not yet implemented.
func (a *GeneralLedgerAdapter) AccountActivity(_ context.Context, _ domain.TenantID, _ int, _ int, _, _ time.Time) ([]domain.VoucherRow, error) {
	return nil, errors.New("general ledger: AccountActivity not implemented")
}

// Vouchers implements domain.GeneralLedger. Not yet implemented.
func (a *GeneralLedgerAdapter) Vouchers(_ context.Context, _ domain.TenantID, _ int, _, _ time.Time) ([]domain.Voucher, error) {
	return nil, errors.New("general ledger: Vouchers not implemented")
}

// VoucherDetail implements domain.GeneralLedger. Not yet implemented.
func (a *GeneralLedgerAdapter) VoucherDetail(_ context.Context, _ domain.TenantID, _ string, _ int) (domain.Voucher, error) {
	return domain.Voucher{}, errors.New("general ledger: VoucherDetail not implemented")
}

// PredefinedAccounts implements domain.GeneralLedger. Not yet implemented.
func (a *GeneralLedgerAdapter) PredefinedAccounts(_ context.Context, _ domain.TenantID) ([]domain.PredefinedAccount, error) {
	return nil, errors.New("general ledger: PredefinedAccounts not implemented")
}
