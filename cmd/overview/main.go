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

	"github.com/mathiasb/cobalt-dingo/internal/adapter/alert"
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

// tokenHeadroom is how much access-token life this command insists on before
// it starts reading. A Fortnox access token lasts an hour, so asking for 15
// minutes costs an occasional early refresh and removes the case where a
// two-minute fetch expires partway through.
const tokenHeadroom = 15 * time.Minute

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

	// Refresh on load, with enough headroom to outlast the read. The first run
	// for a year makes one request per voucher — about two minutes for 405 —
	// and OAuthToken.Valid()'s 30-second margin would let the token die
	// mid-fetch after several hundred requests.
	tokens := adapterfortnox.NewFortnoxRefreshingTokenStore(
		postgres.NewTokenStore(store, cipher), cfg, tokenHeadroom, log)

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

	customerLdg := adapterfortnox.NewCustomerLedgerAdapter(cfg.BaseURL(), tokens, true)
	supplierLdg := adapterfortnox.NewSupplierLedgerAdapter(cfg.BaseURL(), tokens, true)

	receivables, err := customerLdg.UnpaidInvoices(ctx, tenant.ID)
	if err != nil {
		return fmt.Errorf("unpaid customer invoices: %w", err)
	}
	payables, err := supplierLdg.UnpaidInvoices(ctx, tenant.ID)
	if err != nil {
		return fmt.Errorf("unpaid supplier invoices: %w", err)
	}

	// Count every invoice status, not just unpaid. Fortnox's `unpaid` filter
	// excludes unbooked invoices, so without this a receivable of zero cannot
	// be told apart from a ledger full of unbooked obligations (#91). Twelve
	// requests, none of which fetches an invoice.
	supplierStates, err := supplierLdg.StateCounts(ctx, tenant.ID)
	if err != nil {
		return fmt.Errorf("supplier invoice states: %w", err)
	}
	customerStates, err := customerLdg.StateCounts(ctx, tenant.ID)
	if err != nil {
		return fmt.Errorf("customer invoice states: %w", err)
	}

	ov, err := domain.BuildFinancialOverview(yearID, accounts, vouchers, receivables, payables)
	if err != nil {
		return fmt.Errorf("build overview: %w", err)
	}
	ov.Obligations = domain.InvoiceStates{Supplier: supplierStates, Customer: customerStates}

	// Fetch the unbooked invoices whenever the counts say there are any. A
	// count is a finding; the invoices are what someone can act on (#91).
	if verdict, _ := ov.Obligations.Assess(); verdict != domain.ObligationsNone {
		if ov.UnbookedSupplier, err = supplierLdg.UnbookedInvoices(ctx, tenant.ID); err != nil {
			return fmt.Errorf("unbooked supplier invoices: %w", err)
		}
		if ov.UnbookedCustomer, err = customerLdg.UnbookedInvoices(ctx, tenant.ID); err != nil {
			return fmt.Errorf("unbooked customer invoices: %w", err)
		}
	}
	// Watch for newly appeared unbooked invoices (#93). Enabled for the
	// scheduled run and off for an ad-hoc one, so reading the report by hand
	// never consumes an alert that the nightly job should have raised.
	alerted := false
	if os.Getenv("OVERVIEW_ALERT_UNBOOKED") == "true" {
		refs := make([]domain.UnbookedRef, 0, len(ov.UnbookedSupplier)+len(ov.UnbookedCustomer))
		for _, inv := range ov.UnbookedSupplier {
			refs = append(refs, domain.UnbookedRef{Kind: domain.UnbookedPayable, InvoiceNumber: inv.InvoiceNumber})
		}
		for _, inv := range ov.UnbookedCustomer {
			refs = append(refs, domain.UnbookedRef{Kind: domain.UnbookedReceivable, InvoiceNumber: inv.InvoiceNumber})
		}
		watch := domain.NewUnbookedWatch(postgres.NewUnbookedStore(store), alert.NewLogAlerter(log))
		change, werr := watch.Check(ctx, tenant.ID, refs)
		if werr != nil {
			return fmt.Errorf("unbooked-invoice watch: %w", werr)
		}
		alerted = change.HasAlert()
		if len(change.Resolved) > 0 {
			log.Info("unbooked invoices resolved since the last run", "count", len(change.Resolved))
		}
	}

	// Names are off unless explicitly enabled, and only by the exact string
	// "true" — the same shape as FORTNOX_<MODE>_ALLOW_WRITES. "1", "yes" and
	// "TRUE" do not enable it, because a value that ALMOST means true is how
	// something gets switched on by accident.
	//
	// This output reaches a Job log, agent sessions read Job logs, and
	// claudewatcher ingests those transcripts into the brain wiki — which the
	// homelab client list must never reach (#94).
	withAgeing := ov.WithAgeing(time.Now())
	if os.Getenv("OVERVIEW_SHOW_COUNTERPARTIES") == "true" {
		log.Warn("counterparty names ENABLED — do not paste this output anywhere that reaches the brain wiki or a cloud API")
		fmt.Print(report.RenderWithNames(withAgeing))
	} else {
		fmt.Print(report.Render(withAgeing))
	}

	// Exit non-zero when the books do not balance. A report nobody reads is
	// how a discrepancy becomes permanent; a failing Job gets noticed.
	if !ov.Balances() {
		return fmt.Errorf("accounting identity does not hold: out by %s", ov.Discrepancy.String())
	}
	// Also non-zero when an alert was raised, so the scheduled run shows as
	// Failed rather than Complete. Same signal as an error, which is a real
	// limitation of having one exit code and no delivery channel: a reader has
	// to open the log to tell an alert from a fault.
	if alerted {
		return errors.New("new unbooked invoice(s) — see the ALERT line above")
	}
	return nil
}
