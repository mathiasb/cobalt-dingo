// Command fortnox-shape prints the field names and JSON types Fortnox
// actually returns, and compares them with the fields our structs read.
//
// Read-only: it fetches one page per endpoint and prints KEYS AND TYPES ONLY,
// never values. That keeps live supplier and customer names out of logs while
// still answering the question.
//
// Why it exists (#92): two production defects came from a field present in one
// place and absent in the other, with no error either way. json.Unmarshal
// ignores unknown keys and leaves absent keys at the zero value, so
// `TotalInvoiceCurrency float64` against a response with no such field yields
// 0.00 — a plausible invoice amount. The vendored OpenAPI spec is a document,
// not the API; this reads the API.
//
// Usage (in-cluster, where the stored token lives):
//
//	DATABASE_URL=... FORTNOX_INTEGRATION_KEY=... FORTNOX_MODE=production fortnox-shape
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"sort"
	"time"

	adapterfortnox "github.com/mathiasb/cobalt-dingo/internal/adapter/fortnox"
	"github.com/mathiasb/cobalt-dingo/internal/adapter/postgres"
	"github.com/mathiasb/cobalt-dingo/internal/apishape"
	"github.com/mathiasb/cobalt-dingo/internal/clitoken"
	"github.com/mathiasb/cobalt-dingo/internal/config"
	"github.com/mathiasb/cobalt-dingo/internal/crypto"
	"github.com/mathiasb/cobalt-dingo/internal/fortnox"
)

// probe is one endpoint to inspect, with the fields our own struct reads.
type probe struct {
	name       string
	path       string
	collection string
	ourFields  []string
}

// probes covers the endpoints whose shape we depend on. ourFields lists the
// json tags of the corresponding Go struct — kept by hand, because the point
// is to detect drift between the two, and generating one from the other would
// make them agree by construction.
var probes = []probe{
	{
		name:       "supplier invoices",
		path:       "/3/supplierinvoices?filter=fullypaid",
		collection: "SupplierInvoices",
		// internal/fortnox.SupplierInvoiceRow
		ourFields: []string{"GivenNumber", "InvoiceNumber", "SupplierNumber", "SupplierName", "Currency", "Total", "Balance", "DueDate", "Booked", "Cancelled"},
	},
	{
		name:       "customer invoices",
		path:       "/3/invoices?filter=fullypaid",
		collection: "Invoices",
		// internal/fortnox.CustomerInvoiceRow
		ourFields: []string{"DocumentNumber", "CustomerNumber", "CustomerName", "Currency", "Total", "Balance", "DueDate", "InvoiceDate", "Booked", "Cancelled", "Sent"},
	},
	{
		name:       "accounts",
		path:       "/3/accounts",
		collection: "Accounts",
		// internal/fortnox.AccountRow
		ourFields: []string{"Number", "Description", "SRU", "Active", "BalanceBroughtForward", "BalanceCarriedForward"},
	},
}

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if err := run(log); err != nil {
		log.Error("fortnox-shape", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	ctx := context.Background()

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	dsn, key := os.Getenv("DATABASE_URL"), os.Getenv("FORTNOX_INTEGRATION_KEY")
	if dsn == "" || key == "" {
		return errors.New("DATABASE_URL and FORTNOX_INTEGRATION_KEY are both required")
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
	tokens := adapterfortnox.NewFortnoxRefreshingTokenStore(
		postgres.NewTokenStore(store, cipher), cfg, 5*time.Minute, log).
		WithRefreshLock(postgres.NewRefreshLock(store, log))
	tok, err := tokens.Load(ctx, tenant.ID)
	if err != nil {
		return fmt.Errorf("load token: %w", err)
	}
	if err := fortnox.AssertCompany(cfg.BaseURL(), tok.AccessToken, tenant.Name); err != nil {
		return fmt.Errorf("confirm company: %w", err)
	}

	var problems int

	for _, p := range probes {
		fmt.Printf("\n=== %s (%s)\n", p.name, p.path)

		raw, err := fetchRaw(cfg.BaseURL()+p.path, tok.AccessToken)
		if err != nil {
			fmt.Printf("  FETCH FAILED: %v\n", err)
			problems++
			continue
		}
		types, err := apishape.RecordTypes(raw, p.collection)
		if err != nil {
			fmt.Printf("  NO SHAPE: %v\n", err)
			problems++
			continue
		}

		keys := make([]string, 0, len(types))
		for k := range types {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		d := apishape.Compare(keys, p.ourFields)

		// The dangerous direction first: a field we decode that Fortnox does
		// not send is silently the zero value of its type.
		if len(d.ReadNotLive) > 0 {
			fmt.Println("  !! WE READ FIELDS FORTNOX DOES NOT SEND (silently zero):")
			for _, k := range d.ReadNotLive {
				fmt.Printf("     %s\n", k)
			}
			problems++
		}

		fmt.Println("  fields we read, and the type Fortnox sends:")
		for _, f := range p.ourFields {
			if t, ok := types[f]; ok {
				fmt.Printf("     %-24s %s\n", f, t)
			}
		}

		fmt.Printf("  fields Fortnox sends that we ignore (%d):\n", len(d.LiveNotRead))
		for _, k := range d.LiveNotRead {
			fmt.Printf("     %-24s %s\n", k, types[k])
		}
	}

	if problems > 0 {
		return fmt.Errorf("%d endpoint(s) diverge from what our structs read", problems)
	}
	return nil
}

// fetchRaw performs one authenticated GET and returns the response body
// verbatim.
//
// fortnox-shape inspects the RAW shape of what Fortnox sends, so no typed
// client method can serve it: decoding into a struct would discard exactly
// the field-and-type information this command exists to compare. It therefore
// carries its own GET, the same way cmd/probe-sandbox does.
func fetchRaw(requestURL, token string) (json.RawMessage, error) {
	req, err := http.NewRequest(http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", requestURL, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: unexpected status %d", requestURL, resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	return json.RawMessage(body), nil
}
