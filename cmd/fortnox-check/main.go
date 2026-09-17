// Command fortnox-check verifies the Fortnox API connection and confirms
// the environment (sandbox vs production) by listing unpaid supplier invoices.
//
// Read-only throughout: every client it builds passes readOnly=true, so the
// write gate in Client.do refuses any non-GET before it reaches the network.
//
// Token source. With DATABASE_URL set it reads the stored token for the
// configured mode — which is the only way to check a connection that was made
// through the web UI, since that token never touches a file. Without it, the
// local token file is used, which is the original CLI path.
//
// Usage:
//
//	source .env && go run ./cmd/fortnox-check          # local token file
//	DATABASE_URL=... FORTNOX_INTEGRATION_KEY=... \
//	  FORTNOX_MODE=production go run ./cmd/fortnox-check
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	adapterfortnox "github.com/mathiasb/cobalt-dingo/internal/adapter/fortnox"
	"github.com/mathiasb/cobalt-dingo/internal/adapter/postgres"
	"github.com/mathiasb/cobalt-dingo/internal/clitoken"
	"github.com/mathiasb/cobalt-dingo/internal/config"
	"github.com/mathiasb/cobalt-dingo/internal/crypto"
	"github.com/mathiasb/cobalt-dingo/internal/domain"
	"github.com/mathiasb/cobalt-dingo/internal/fortnox"
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))

	cfg, err := config.Load()
	if err != nil {
		log.Error("config", "err", err)
		os.Exit(1)
	}

	token, err := loadValidToken(cfg, log)
	if err != nil {
		log.Error("token", "err", err)
		os.Exit(1)
	}

	count, err := unpaidSupplierInvoiceCount(cfg.BaseURL(), token.AccessToken)
	if err != nil {
		log.Error("supplierinvoices", "err", err)
		os.Exit(1)
	}

	fmt.Println("─────────────────────────────────────")
	fmt.Printf("  Mode                 : %s\n", cfg.Mode.Label())
	fmt.Printf("  Token file           : %s\n", cfg.Mode.TokenFile())
	fmt.Printf("  Base URL             : %s\n", cfg.BaseURL())
	fmt.Printf("  Writes allowed       : %v\n", cfg.AllowsWrites)
	fmt.Printf("  Unpaid invoices      : %d\n", count)
	fmt.Printf("  Inbox (Arkivplats)   : %s\n", inboxStatus(cfg.BaseURL(), token.AccessToken, cfg.InvoiceInbox))
	// Which company is this, really? Two companies on this account are both
	// named "Definitely Mabe AB" and share organisation number 556836-0688, so
	// the name proves nothing (#87). The financial-year list is the cheapest
	// discriminator available: a company that has traded for years has several,
	// a freshly created one has one.
	fmt.Printf("  Financial years      : %s\n", financialYearSummary(cfg, token.AccessToken))
	// Counted across every documented filter, because "unpaid" has a specific
	// meaning in Fortnox: an invoice must be bookkept before it is a liability.
	// Invoices sitting unbooked or awaiting approval do not appear under
	// `unpaid`, so a zero there is not evidence that there are no invoices —
	// it is evidence about one state, and reporting it alone asks the wrong
	// question.
	fmt.Printf("  Supplier invoices    : %s\n", supplierInvoiceStates(cfg.BaseURL(), token.AccessToken))
	if acct := os.Getenv("FORTNOX_CHECK_ACCOUNT"); acct != "" {
		fmt.Printf("  Account %-13s: %s\n", acct, accountStatus(cfg, token.AccessToken, acct))
	}
	fmt.Println("─────────────────────────────────────")

	switch cfg.Mode {
	case config.ModeSandbox:
		fmt.Println("✓ Connected to SANDBOX — safe to write")
	case config.ModeProduction:
		if cfg.AllowsWrites {
			fmt.Println("✓ Connected to PRODUCTION — writes enabled")
		} else {
			fmt.Println("⚠ Connected to PRODUCTION — read-only mode (set FORTNOX_PRODUCTION_ALLOW_WRITES=true to enable writes)")
		}
	}
}

func loadValidToken(cfg config.Fortnox, log *slog.Logger) (fortnox.Token, error) {
	// A connection made through the web UI stores its token in postgres and
	// never writes a file, so the file path cannot check the deployed state at
	// all. When a database is configured, that is the authoritative source.
	if dsn := os.Getenv("DATABASE_URL"); dsn != "" {
		t, err := tokenFromPostgres(dsn, cfg, log)
		if err != nil {
			return fortnox.Token{}, err
		}
		return t, nil
	}

	tokenPath := cfg.Mode.TokenFile()
	t, err := fortnox.LoadToken(tokenPath)
	if err != nil {
		return fortnox.Token{}, fmt.Errorf("no saved token at %s — run fortnox-auth for mode %s: %w", tokenPath, cfg.Mode, err)
	}
	if fortnox.TokenValid(t) {
		return t, nil
	}
	log.Info("access token expired, refreshing")
	t, err = fortnox.RefreshAccessToken(cfg.ClientID, cfg.ClientSecret, t.RefreshToken)
	if err != nil {
		return fortnox.Token{}, fmt.Errorf("refresh failed — re-run fortnox-auth for mode %s: %w", cfg.Mode, err)
	}
	if err := fortnox.SaveToken(tokenPath, t); err != nil {
		log.Warn("could not save refreshed token", "err", err)
	}
	return t, nil
}

func unpaidSupplierInvoiceCount(baseURL, token string) (int, error) {
	req, err := http.NewRequest(http.MethodGet, baseURL+"/3/supplierinvoices?filter=unpaid", nil)
	if err != nil {
		return 0, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("GET supplierinvoices: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}

	var envelope struct {
		MetaInformation struct {
			TotalResources int `json:"@TotalResources"`
		} `json:"MetaInformation"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return 0, fmt.Errorf("decode: %w", err)
	}
	return envelope.MetaInformation.TotalResources, nil
}

// inboxStatus probes /3/inbox and reports in one line.
//
// It exists to answer a question that is otherwise only answerable by trying:
// was the `inbox` scope actually granted? Scopes belong to the integration, so
// adding one means re-authorizing every connected company — an expensive thing
// to discover late, and the reason this is checked rather than assumed.
//
// The arkivplats address is compared, never printed: it embeds the
// organisation number, and this output reaches CI logs (#80 keeps org numbers
// out of the repo for the same reason). Whether it MATCHES is the useful half
// anyway — a mismatch is the stale-address bug that silently swallowed ten
// forwards.
func inboxStatus(baseURL, token, configured string) string {
	folder, err := fortnox.NewClient(baseURL, token, true).GetInbox()
	if err != nil {
		if errors.Is(err, fortnox.ErrInboxScopeMissing) {
			return "SCOPE MISSING — add `inbox` to the integration and re-authorize"
		}
		return "unreachable: " + err.Error()
	}

	files := len(folder.Files)
	for _, sub := range folder.Folders {
		files += len(sub.Files)
	}

	match := "not configured"
	if configured != "" {
		match = "does NOT match INVOICE_INBOX — see #80"
		if strings.EqualFold(strings.TrimSpace(configured), strings.TrimSpace(folder.Email)) {
			match = "matches INVOICE_INBOX"
		}
		for _, sub := range folder.Folders {
			if strings.EqualFold(strings.TrimSpace(configured), strings.TrimSpace(sub.Email)) {
				match = "matches INVOICE_INBOX"
			}
		}
	}
	return fmt.Sprintf("reachable, %d file(s), address %s", files, match)
}

// accountStatus answers assumption A1: is anything still arriving in a GL
// account? Reported as a count and a date span, which is what can be compared
// against the bank's own record — no Fortnox endpoint exposes the bank side, so
// the comparison is always against a human looking at their internet bank.
//
// Scoped to the latest financial year. A year boundary would otherwise make an
// active account look dormant every January.
func accountStatus(cfg config.Fortnox, token, acct string) string {
	num, err := strconv.Atoi(strings.TrimSpace(acct))
	if err != nil {
		return fmt.Sprintf("not a number: %q", acct)
	}

	tenant := domain.TenantID("check")
	store := staticTokenStore{token: domain.OAuthToken{AccessToken: token, ExpiresAt: time.Now().Add(time.Hour)}}
	gl := adapterfortnox.NewGeneralLedgerAdapter(cfg.BaseURL(), store, true)

	ctx := context.Background()
	years, err := gl.FinancialYears(ctx, tenant)
	if err != nil {
		return "financial years unreadable: " + err.Error()
	}
	if len(years) == 0 {
		return "no financial years in Fortnox"
	}
	latest := years[0]
	for _, y := range years {
		if y.From.After(latest.From) {
			latest = y
		}
	}

	vouchers, err := gl.Vouchers(ctx, tenant, latest.ID, latest.From, latest.To)
	if err != nil {
		return "vouchers unreadable: " + err.Error()
	}
	// How many of those vouchers actually carry rows. Fortnox's list endpoint
	// declares VoucherRows in the spec but commonly returns them empty —
	// rows arrive from the per-voucher detail endpoint. If none carry rows then
	// an account check over this data is structurally blind and CANNOT find
	// activity, whatever the books contain. Reporting it is the difference
	// between an answer and a coincidence.
	withRows := 0
	for _, v := range vouchers {
		if len(v.Rows) > 0 {
			withRows++
		}
	}
	if len(vouchers) > 0 && withRows == 0 {
		// Rows are unavailable from the list endpoint, so fall back to the
		// account's own balances. Brought-forward against carried-forward
		// answers assumption A1's real question — is anything moving through
		// this account — without needing one detail request per voucher.
		return fmt.Sprintf("rows unavailable from the voucher list (%d vouchers, 0 with rows); %s",
			len(vouchers), accountBalanceLine(gl, ctx, tenant, latest.ID, num))
	}

	sum := domain.SummariseAccount(vouchers, num)

	// The total matters as much as the account's own count. An empty account
	// in a year with hundreds of vouchers means nothing feeds THIS account; an
	// empty account in a year with no vouchers at all means the books simply
	// have not been written up yet. Those need opposite responses, and
	// reporting only the account's count cannot tell them apart — which is
	// exactly the ambiguity the first run of this check produced.
	if sum.Count == 0 {
		if len(vouchers) == 0 {
			return fmt.Sprintf("no vouchers AT ALL in %s–%s — the year is unbooked, so this says nothing about the account",
				latest.From.Format("2006-01-02"), latest.To.Format("2006-01-02"))
		}
		return fmt.Sprintf("NO activity, though the year holds %d voucher(s) of which %d carry rows — nothing feeds this account",
			len(vouchers), withRows)
	}
	return fmt.Sprintf("%d of %d voucher(s) (%d carry rows), %s → %s",
		sum.Count, len(vouchers), withRows, sum.Earliest, sum.Latest)
}

// staticTokenStore serves one already-loaded token. The ledger adapters take a
// TokenStore because the server has many tenants; this command has one token
// and no reason to reach for the database twice.
type staticTokenStore struct{ token domain.OAuthToken }

func (s staticTokenStore) Load(context.Context, domain.TenantID) (domain.OAuthToken, error) {
	return s.token, nil
}

func (s staticTokenStore) Save(context.Context, domain.TenantID, domain.OAuthToken) error {
	return errors.New("fortnox-check never writes tokens")
}

func (s staticTokenStore) AtomicRefresh(context.Context, domain.TenantID, domain.OAuthToken, domain.OAuthToken) error {
	return errors.New("fortnox-check never writes tokens")
}

func (s staticTokenStore) Delete(context.Context, domain.TenantID) error {
	return errors.New("fortnox-check never writes tokens")
}

// tokenFromPostgres reads the stored token for the configured mode.
//
// Refreshing is deliberately left to the server. This command is a read-only
// check, and a CLI that refreshed would consume the rotating refresh token out
// from under the running pod — Fortnox rotates it on every use, so two writers
// is how a live connection dies (#37).
func tokenFromPostgres(dsn string, cfg config.Fortnox, log *slog.Logger) (fortnox.Token, error) {
	raw := os.Getenv("FORTNOX_INTEGRATION_KEY")
	if raw == "" {
		return fortnox.Token{}, errors.New("DATABASE_URL is set but FORTNOX_INTEGRATION_KEY is not: stored tokens are encrypted at rest and cannot be read without it")
	}
	cipher, err := crypto.NewCipher(raw)
	if err != nil {
		return fortnox.Token{}, fmt.Errorf("FORTNOX_INTEGRATION_KEY unusable: %w", err)
	}
	store, err := postgres.NewStore(dsn)
	if err != nil {
		return fortnox.Token{}, fmt.Errorf("connect to postgres: %w", err)
	}
	defer func() { _ = store.Close() }()

	tenants, err := postgres.NewTenantRepo(store).ListByPrefix(context.Background(), "")
	if err != nil {
		return fortnox.Token{}, fmt.Errorf("list tenants: %w", err)
	}

	// Shared with cmd/overview. This used to take the first tenant whose ID
	// contained the mode, which silently picked one of several connected
	// companies — and two companies on this account share organisation number
	// 556836-0688 (#87), so the pick looked like a choice.
	tenant, err := clitoken.SelectTenant(tenants, string(cfg.Mode), os.Getenv("FORTNOX_COMPANY"))
	if err != nil {
		return fortnox.Token{}, err
	}

	tokens := postgres.NewTokenStore(store, cipher)
	tok, err := tokens.Load(context.Background(), tenant.ID)
	if err != nil {
		return fortnox.Token{}, fmt.Errorf("load token for %s: %w", tenant.ID, err)
	}
	// Named, so the operator can see WHICH company was checked.
	log.Info("using stored token", "tenant", tenant.ID, "company", tenant.Name)
	return fortnox.Token{AccessToken: tok.AccessToken, RefreshToken: tok.RefreshToken, ExpiresAt: tok.ExpiresAt}, nil
}

// financialYearSummary lists the financial years the connected company has.
//
// Printed because "which company am I actually talking to" is not answerable
// from the company name here, and the year list distinguishes a company with
// history from one created last week without revealing anything sensitive.
func financialYearSummary(cfg config.Fortnox, token string) string {
	store := staticTokenStore{token: domain.OAuthToken{AccessToken: token, ExpiresAt: time.Now().Add(time.Hour)}}
	gl := adapterfortnox.NewGeneralLedgerAdapter(cfg.BaseURL(), store, true)

	years, err := gl.FinancialYears(context.Background(), domain.TenantID("check"))
	if err != nil {
		return "unreadable: " + err.Error()
	}
	if len(years) == 0 {
		return "none — this company has no financial years at all"
	}
	parts := make([]string, 0, len(years))
	for _, y := range years {
		parts = append(parts, fmt.Sprintf("id=%d %s→%s", y.ID, y.From.Format("2006-01-02"), y.To.Format("2006-01-02")))
	}
	return fmt.Sprintf("%d: %s", len(years), strings.Join(parts, ", "))
}

// supplierInvoiceStates reports the count under each documented filter value,
// through the typed counter on the client.
//
// The enum comes from the vendored OpenAPI spec rather than from guessing:
// cancelled, fullypaid, unpaid, unpaidoverdue, unbooked, pendingpayment,
// authorizepending.
func supplierInvoiceStates(baseURL, token string) string {
	counts, err := fortnox.NewClient(baseURL, token, true).SupplierInvoiceStateCounts()
	if err != nil {
		return "unreadable: " + err.Error()
	}
	parts := make([]string, 0, len(counts))
	for _, f := range fortnox.SupplierInvoiceFilters {
		parts = append(parts, fmt.Sprintf("%s=%d", f, counts[f]))
	}
	return strings.Join(parts, " ")
}

// accountBalanceLine reports an account's opening and closing balance.
//
// The balances come from /3/accounts, which carries BalanceBroughtForward and
// BalanceCarriedForward per account and needs no voucher rows. A difference
// between them means money moved through the account during the year, which is
// what assumption A1 is really asking about; equality means nothing did.
//
// Deliberately NOT routed through AccountBalances, which drops any account
// whose carried-forward balance is zero — the exact case that would need
// reporting here.
func accountBalanceLine(gl *adapterfortnox.GeneralLedgerAdapter, ctx context.Context, tenant domain.TenantID, yearID, num int) string {
	accounts, err := gl.ChartOfAccounts(ctx, tenant, yearID)
	if err != nil {
		return "balances unreadable: " + err.Error()
	}
	for _, a := range accounts {
		if a.Number != num {
			continue
		}
		// BalanceCarriedForward is written when a financial year is CLOSED. For
		// an open year it is zero, which means "not set yet" and not "the
		// account is empty" — so comparing it against the opening balance
		// cannot show movement. Claiming it did was this check's fourth wrong
		// answer about account 1930 in one session, all four from measuring
		// something other than the question.
		if a.BalanceCF.MinorUnits == 0 {
			return fmt.Sprintf("opening %s; carried-forward not set — the year is open, so this cannot show movement either way",
				a.BalanceBF.String())
		}
		verdict := "NO movement"
		if a.BalanceBF.MinorUnits != a.BalanceCF.MinorUnits {
			verdict = "MOVED during the year"
		}
		return fmt.Sprintf("opening %s → closing %s — %s", a.BalanceBF.String(), a.BalanceCF.String(), verdict)
	}
	return fmt.Sprintf("account %d is not in the chart of accounts", num)
}
