// Command overview prints the company's financial position for one financial
// year: derived account balances, the balance sheet, the result, and
// receivable/payable ageing.
//
// Read-only throughout. Every client it builds is read-only, so the write gate
// in Client.do refuses any non-GET before it reaches the network, and the
// voucher source takes no readOnly flag at all.
//
// Cost. The first run for a year reads every voucher's rows — one request per
// voucher, roughly two minutes for a 405-voucher year under the Fortnox rate
// limit. Later runs serve from the cache once its completeness has been
// verified against Fortnox's own count in a single request (ADR-0006).
//
// Usage (in-cluster, where the stored token lives):
//
//	DATABASE_URL=... FORTNOX_INTEGRATION_KEY=... FORTNOX_MODE=production \
//	  FORTNOX_YEAR=2 [FORTNOX_COMPANY="Definitely Mabe AB"] overview
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"time"

	adapterfortnox "github.com/mathiasb/cobalt-dingo/internal/adapter/fortnox"
	"github.com/mathiasb/cobalt-dingo/internal/adapter/postgres"
	"github.com/mathiasb/cobalt-dingo/internal/clitoken"
	"github.com/mathiasb/cobalt-dingo/internal/config"
	"github.com/mathiasb/cobalt-dingo/internal/crypto"
	"github.com/mathiasb/cobalt-dingo/internal/domain"
	"github.com/mathiasb/cobalt-dingo/internal/fortnox"
	"github.com/mathiasb/cobalt-dingo/internal/report"
)

// cacheMaxAge is the age backstop. Completeness is checked on every run
// against Fortnox's own count, so this exists only to catch the case that
// check cannot see: a voucher deleted AND replaced between runs, leaving the
// total unchanged. Rare enough to measure in hours, not minutes.
const cacheMaxAge = 12 * time.Hour

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if err := run(log); err != nil {
		log.Error("overview", "err", err)
		os.Exit(1)
	}
}

// resolveYear returns the financial year to report on: FORTNOX_YEAR when set
// explicitly, otherwise the year containing today.
func resolveYear(ctx context.Context, gl *adapterfortnox.GeneralLedgerAdapter, tenantID domain.TenantID) (int, error) {
	if raw := os.Getenv("FORTNOX_YEAR"); raw != "" {
		id, err := strconv.Atoi(raw)
		if err != nil {
			return 0, fmt.Errorf("FORTNOX_YEAR must be a financial year ID: %w", err)
		}
		return id, nil
	}
	years, err := gl.FinancialYears(ctx, tenantID)
	if err != nil {
		return 0, fmt.Errorf("list financial years: %w", err)
	}
	year, err := clitoken.SelectFinancialYear(years, time.Now())
	if err != nil {
		return 0, err
	}
	return year.ID, nil
}

func run(log *slog.Logger) error {
	ctx := context.Background()

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return errors.New("DATABASE_URL is required: the cache and the stored token both live in postgres")
	}
	key := os.Getenv("FORTNOX_INTEGRATION_KEY")
	if key == "" {
		return errors.New("FORTNOX_INTEGRATION_KEY is required: stored tokens are encrypted at rest")
	}

	cipher, err := crypto.NewCipher(key)
	if err != nil {
		return fmt.Errorf("FORTNOX_INTEGRATION_KEY unusable: %w", err)
	}
	store, err := postgres.NewStore(dsn)
	if err != nil {
		return fmt.Errorf("connect to postgres: %w", err)
	}
	defer func() { _ = store.Close() }()

	all, err := postgres.NewTenantRepo(store).ListByPrefix(ctx, "")
	if err != nil {
		return fmt.Errorf("list tenants: %w", err)
	}
	tenant, err := clitoken.SelectTenant(all, string(cfg.Mode), os.Getenv("FORTNOX_COMPANY"))
	if err != nil {
		return err
	}
	// Named in the log, because "which company did this report describe" must
	// be answerable from the output alone (#87).
	log.Info("reading", "tenant", tenant.ID, "company", tenant.Name, "mode", cfg.Mode)

	tokens := postgres.NewTokenStore(store, cipher)
	tok, err := tokens.Load(ctx, tenant.ID)
	if err != nil {
		return fmt.Errorf("load token for %s: %w", tenant.ID, err)
	}

	// Ask Fortnox which company this token actually opens, and refuse if it
	// disagrees with the record we selected on. The mode is a value in our own
	// process and the stored name was captured at connect time; only Fortnox
	// knows whose books these are now. Free to check, and the failure it
	// catches — a report describing the wrong company — is silent otherwise.
	if err := fortnox.AssertCompany(cfg.BaseURL(), tok.AccessToken, tenant.Name); err != nil {
		return fmt.Errorf("confirm company for tenant %s: %w", tenant.ID, err)
	}
	fmt.Printf("Company: %s (confirmed with Fortnox)\n", tenant.Name)

	gl := adapterfortnox.NewGeneralLedgerAdapter(cfg.BaseURL(), tokens, true)

	// Resolve the year rather than taking an ID. Fortnox's financial-year IDs
	// are opaque and not in date order, so a hand-supplied number is a guess
	// with a plausible wrong answer — and a report against the wrong year looks
	// entirely reasonable.
	yearID, err := resolveYear(ctx, gl, tenant.ID)
	if err != nil {
		return err
	}
	log.Info("financial year", "id", yearID)

	accounts, err := gl.ChartOfAccounts(ctx, tenant.ID, yearID)
	if err != nil {
		return fmt.Errorf("chart of accounts: %w", err)
	}

	vouchers, err := domain.NewVoucherService(
		postgres.NewVoucherCache(store),
		adapterfortnox.NewVoucherSourceAdapter(cfg.BaseURL(), tokens),
		cacheMaxAge,
		time.Now,
	).Vouchers(ctx, tenant.ID, yearID)
	if err != nil {
		return fmt.Errorf("vouchers for year %d: %w", yearID, err)
	}
	log.Info("vouchers", "count", len(vouchers.Vouchers), "freshness", vouchers.Freshness, "reason", vouchers.Reason)

	receivables, err := adapterfortnox.NewCustomerLedgerAdapter(cfg.BaseURL(), tokens, true).UnpaidInvoices(ctx, tenant.ID)
	if err != nil {
		return fmt.Errorf("unpaid customer invoices: %w", err)
	}
	payables, err := adapterfortnox.NewSupplierLedgerAdapter(cfg.BaseURL(), tokens, true).UnpaidInvoices(ctx, tenant.ID)
	if err != nil {
		return fmt.Errorf("unpaid supplier invoices: %w", err)
	}

	ov, err := domain.BuildFinancialOverview(yearID, accounts, vouchers, receivables, payables)
	if err != nil {
		return fmt.Errorf("build overview: %w", err)
	}
	fmt.Print(report.Render(ov.WithAgeing(time.Now())))

	// Exit non-zero when the books do not balance. A report nobody reads is
	// how a discrepancy becomes permanent; a failing Job gets noticed.
	if !ov.Balances() {
		return fmt.Errorf("accounting identity does not hold: out by %s", ov.Discrepancy.String())
	}
	return nil
}
