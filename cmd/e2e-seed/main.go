// Command e2e-seed creates realistic test suppliers and FCY supplier invoices
// in the Fortnox sandbox for end-to-end testing.
//
// All records are tagged with the [E2E] prefix in their name so e2e-teardown
// can identify and remove them cleanly.
//
// Seed covers four scenarios:
//
//	Müller GmbH      — EUR invoice, IBAN set    → should be enriched and batched
//	Van der Berg BV  — EUR invoice, IBAN set    → should be enriched and batched (overdue)
//	Nordic AB        — SEK invoice, no IBAN     → should be filtered (domestic SEK)
//	No-IBAN Corp     — USD invoice, no IBAN     → should be skipped (foreign, missing IBAN)
//
// Usage:
//
//	source .env && go run ./cmd/e2e-seed
package main

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/mathiasb/cobalt-dingo/internal/config"
	"github.com/mathiasb/cobalt-dingo/internal/fortnox"
)

const e2ePrefix = "E2E-"

type seedSupplier struct {
	name     string
	country  string
	currency string
	iban     string
	bic      string
}

type seedInvoice struct {
	supplierName string
	currency     string
	total        float64
	invoiceDate  string
	dueDate      string
	description  string
}

var suppliers = []seedSupplier{
	{
		name:     e2ePrefix + "Müller GmbH",
		country:  "DE",
		currency: "EUR",
		iban:     "DE89370400440532013000",
		bic:      "COBADEFFXXX",
	},
	{
		name:     e2ePrefix + "Van der Berg BV",
		country:  "NL",
		currency: "EUR",
		iban:     "NL91ABNA0417164300",
		bic:      "ABNANL2A",
	},
	{
		name:     e2ePrefix + "Nordic Supplies AB",
		country:  "SE",
		currency: "SEK",
		iban:     "", // domestic SEK — no IBAN needed for filtering test
		bic:      "",
	},
	{
		name:     e2ePrefix + "No-IBAN Corp",
		country:  "US",
		currency: "USD",
		iban:     "", // foreign FCY but no IBAN — enrichment skip test
		bic:      "",
	},
}

var invoices = []seedInvoice{
	{
		supplierName: e2ePrefix + "Müller GmbH",
		currency:     "EUR",
		total:        2450.00,
		invoiceDate:  "2026-04-01",
		dueDate:      "2026-04-30",
		description:  e2ePrefix + "Consulting services Q1",
	},
	{
		supplierName: e2ePrefix + "Van der Berg BV",
		currency:     "EUR",
		total:        1200.00,
		invoiceDate:  "2026-03-15",
		dueDate:      "2026-04-14", // overdue
		description:  e2ePrefix + "Software license renewal",
	},
	{
		supplierName: e2ePrefix + "Nordic Supplies AB",
		currency:     "SEK",
		total:        15000.00,
		invoiceDate:  "2026-04-01",
		dueDate:      "2026-05-01",
		description:  e2ePrefix + "Office supplies",
	},
	{
		supplierName: e2ePrefix + "No-IBAN Corp",
		currency:     "USD",
		total:        3500.00,
		invoiceDate:  "2026-04-05",
		dueDate:      "2026-04-25",
		description:  e2ePrefix + "Cloud infrastructure",
	},
}

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))

	cfg, err := config.Load()
	if err != nil {
		log.Error("config", "err", err)
		os.Exit(1)
	}
	if !cfg.IsSandbox() {
		log.Error("refusing to seed production — set FORTNOX_ENV=sandbox")
		os.Exit(1)
	}

	token, err := loadValidToken(cfg, log)
	if err != nil {
		log.Error("token", "err", err)
		os.Exit(1)
	}

	client := fortnox.NewClient(cfg.BaseURL(), token.AccessToken)

	// Build name→number index: reactivate existing E2E suppliers, create missing ones.
	supplierNumbers := make(map[string]int, len(suppliers))
	existing, err := client.ListSuppliers(e2ePrefix)
	if err != nil {
		log.Error("list existing suppliers", "err", err)
		os.Exit(1)
	}
	for _, s := range existing {
		supplierNumbers[s.Name] = s.SupplierNumber
	}

	fmt.Println("Setting up suppliers...")
	for _, s := range suppliers {
		if num, found := supplierNumbers[s.name]; found {
			if err := client.ActivateSupplier(num); err != nil {
				log.Error("reactivate supplier", "name", s.name, "err", err)
				os.Exit(1)
			}
			fmt.Printf("  ↺ %-35s → supplier #%d (reactivated)\n", s.name, num)
		} else {
			num, err := client.CreateSupplier(fortnox.SupplierCreate{
				Name:        s.name,
				CountryCode: s.country,
				IBAN:        s.iban,
				BIC:         s.bic,
			})
			if err != nil {
				log.Error("create supplier", "name", s.name, "err", err)
				os.Exit(1)
			}
			supplierNumbers[s.name] = num
			fmt.Printf("  ✓ %-35s → supplier #%d (created)\n", s.name, num)
		}
	}

	// Create invoices.
	fmt.Println("\nCreating supplier invoices...")
	for _, inv := range invoices {
		snum, ok := supplierNumbers[inv.supplierName]
		if !ok {
			log.Error("unknown supplier", "name", inv.supplierName)
			os.Exit(1)
		}
		givenNumber, err := client.CreateSupplierInvoice(fortnox.SupplierInvoiceCreate{
			SupplierNumber: snum,
			InvoiceDate:    inv.invoiceDate,
			DueDate:        inv.dueDate,
			Currency:       inv.currency,
			Description:    inv.description,
			Total:          inv.total,
		})
		if err != nil {
			log.Error("create invoice", "supplier", inv.supplierName, "err", err)
			os.Exit(1)
		}
		if err := client.BookkeepSupplierInvoice(givenNumber); err != nil {
			log.Error("bookkeep invoice", "invoice", givenNumber, "err", err)
			os.Exit(1)
		}
		fmt.Printf("  ✓ %-35s %s %8.2f  due %s  → invoice %s (booked)\n",
			inv.supplierName, inv.currency, inv.total, inv.dueDate, givenNumber)
	}

	fmt.Println("\nSeed complete. Run: source .env && go run ./cmd/e2e-teardown")
}

func loadValidToken(cfg config.Fortnox, log *slog.Logger) (fortnox.Token, error) {
	t, err := fortnox.LoadToken()
	if err != nil {
		return fortnox.Token{}, fmt.Errorf("no saved token — run fortnox-auth first: %w", err)
	}
	if t.Valid() {
		return t, nil
	}
	log.Info("access token expired, refreshing")
	t, err = fortnox.RefreshAccessToken(cfg.ClientID, cfg.ClientSecret, t.RefreshToken)
	if err != nil {
		return fortnox.Token{}, fmt.Errorf("refresh failed — re-run fortnox-auth: %w", err)
	}
	if err := fortnox.SaveToken(t); err != nil {
		log.Warn("could not save refreshed token", "err", err)
	}
	return t, nil
}
