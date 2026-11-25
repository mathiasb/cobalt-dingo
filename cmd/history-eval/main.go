// Command history-eval measures how well a company's own supplier-invoice
// history predicts how its next invoices are booked, and which Fortnox fields
// are present in the real data (ADR-0013, spike 1, issue #155).
//
// It prints AGGREGATES ONLY: counts, rates and two dates. No supplier name,
// number, amount or account is printed, and the report type cannot hold one.
// That matters here because Job logs are read by agent sessions and ingested
// into the brain wiki (#94); the ledger lines of the books must not travel
// that way.
//
// Read-only throughout: every client is built read-only, so the write gate in
// Client.do refuses any non-GET before it reaches the network.
//
// Usage (in-cluster, where the stored token lives), same wiring as cmd/overview:
//
//	DATABASE_URL=... FORTNOX_INTEGRATION_KEY=... FORTNOX_MODE=production \
//	  [FORTNOX_COMPANY="Definitely Mabe AB"] [HISTORY_FROM=2023-01-01] \
//	  [HISTORY_TO=2026-12-31] history-eval
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"os"
	"time"

	adapterfortnox "github.com/mathiasb/cobalt-dingo/internal/adapter/fortnox"
	"github.com/mathiasb/cobalt-dingo/internal/adapter/postgres"
	"github.com/mathiasb/cobalt-dingo/internal/clitoken"
	"github.com/mathiasb/cobalt-dingo/internal/config"
	"github.com/mathiasb/cobalt-dingo/internal/crypto"
	"github.com/mathiasb/cobalt-dingo/internal/fortnox"
	"github.com/mathiasb/cobalt-dingo/internal/historyeval"
)

// tokenHeadroom outlasts the read: roughly one request per invoice at
// Fortnox's 25 per 5 seconds.
const tokenHeadroom = 15 * time.Minute

// testFraction is the share of usable invoices, latest by date, held out.
const testFraction = 0.2

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if err := run(log); err != nil {
		log.Error("history-eval", "err", err)
		os.Exit(1)
	}
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// safe turns an error from Fortnox or the tenant lookup into one that names
// only a class of failure. Those errors can carry an invoice number in a URL, a
// raw value that failed to decode, or a company name, and the Job log is read by
// agent sessions and ingested into a brain wiki (#94).
func safe(what string, err error) error {
	return fmt.Errorf("%s: %s", what, historyeval.SafeError(err))
}

func run(log *slog.Logger) error {
	ctx := context.Background()

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return errors.New("DATABASE_URL is required: the stored token lives in postgres")
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
		// Fixed text on purpose: a malformed DSN is echoed back by the driver,
		// password included, and this error is logged.
		return errors.New("connect to postgres failed (details withheld: the driver's error can contain the DSN)")
	}
	defer func() { _ = store.Close() }()

	all, err := postgres.NewTenantRepo(store).ListByPrefix(ctx, "")
	if err != nil {
		return fmt.Errorf("list tenants: %w", err)
	}
	tenant, err := clitoken.SelectTenant(all, string(cfg.Mode), os.Getenv("FORTNOX_COMPANY"))
	if err != nil {
		// The detail lists company names; say what to do without them.
		return errors.New("select tenant failed: no single tenant could be chosen for this mode (set FORTNOX_COMPANY to the company name, or check that exactly one tenant is connected)")
	}
	// The mode only. The tenant id embeds the organisation number, a registry
	// lookup away from the company name, and names stay out of Job logs (#94).
	log.Info("reading", "mode", cfg.Mode)

	// The token store and the refresh lock log the tenant id (which embeds the
	// organisation number) when two processes refresh at once or a lock release
	// fails. They get a logger that discards, so those lines never reach the Job log.
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	tokens := adapterfortnox.NewFortnoxRefreshingTokenStore(
		postgres.NewTokenStore(store, cipher), cfg, tokenHeadroom, quiet).
		WithRefreshLock(postgres.NewRefreshLock(store, quiet))
	tok, err := tokens.Load(ctx, tenant.ID)
	if err != nil {
		return safe("load token", err)
	}
	// Ask Fortnox whose books this token opens and refuse if it disagrees with
	// the record selected on: a report describing the wrong company is silent.
	if err := fortnox.AssertCompany(cfg.BaseURL(), tok.AccessToken, tenant.Name); err != nil {
		return safe("confirm company", err)
	}
	fmt.Println("company confirmed with Fortnox: yes")

	client := fortnox.NewClient(cfg.BaseURL(), tok.AccessToken, true)

	from := env("HISTORY_FROM", time.Now().AddDate(-3, 0, 0).Format("2006-01-02"))
	to := env("HISTORY_TO", time.Now().Format("2006-01-02"))

	heads, err := client.ListSupplierInvoiceHeads(from, to)
	if err != nil {
		return safe("list supplier invoices", err)
	}
	log.Info("supplier invoices listed", "count", len(heads), "from", from, "to", to)

	invoices := make([]historyeval.Invoice, 0, len(heads))
	for i, h := range heads {
		inv := historyeval.Invoice{
			SupplierNumber: h.SupplierNumber, SupplierName: h.SupplierName, Date: h.InvoiceDate,
			Currency: h.Currency, Booked: h.Booked, Cancelled: h.Cancelled,
			VoucherLinked: h.VoucherNumber != 0,
		}
		if !h.Cancelled {
			rows, err := client.GetSupplierInvoiceRows(h.GivenNumber)
			if err != nil {
				return safe(fmt.Sprintf("read invoice %d of %d", i+1, len(heads)), err)
			}
			for _, r := range rows {
				inv.Rows = append(inv.Rows, historyeval.Row{
					Account: r.Account, Debit: minor(r.Debit), Credit: minor(r.Credit),
					CostCenter: r.CostCenter != "", Project: r.Project != "",
				})
			}
		}
		invoices = append(invoices, inv)
		if (i+1)%25 == 0 {
			log.Info("rows read", "done", i+1, "of", len(heads))
		}
	}

	ids, err := client.ListSupplierIdentities()
	if err != nil {
		return safe("list suppliers", err)
	}
	suppliers := make([]historyeval.Supplier, 0, len(ids))
	for _, id := range ids {
		suppliers = append(suppliers, historyeval.Supplier{
			Number: id.SupplierNumber, HasOrganisationNumber: id.HasOrganisationNumber, HasVATNumber: id.HasVATNumber,
			HasBankAccount: id.HasBankAccount, HasIBAN: id.HasIBAN, PreDefinedAccount: id.PreDefinedAccount,
		})
	}

	fmt.Print(historyeval.Render(historyeval.Analyse(invoices, suppliers, testFraction)))
	return nil
}

// minor converts a base-currency amount to minor units, rounding half away from zero.
func minor(v float64) int64 { return int64(math.Round(v * 100)) }
