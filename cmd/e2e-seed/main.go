// Command e2e-seed creates realistic test data in the Fortnox sandbox covering
// the AP, AR, project, cost-center and asset surfaces of the v0.5.0 financial
// command center.
//
// All records are tagged with the E2E- prefix in their name so e2e-teardown
// can identify and remove them cleanly.
//
// AP scenarios (suppliers + supplier invoices):
//
//	Müller GmbH      — EUR invoice, IBAN set    → should be enriched and batched
//	Van der Berg BV  — EUR invoice, IBAN set    → should be enriched and batched (overdue)
//	Nordic AB        — SEK invoice, no IBAN     → should be filtered (domestic SEK)
//	No-IBAN Corp     — USD invoice, no IBAN     → should be skipped (foreign, missing IBAN)
//
// AR scenarios (customers + customer invoices):
//
//	5 customers across NO/DE/FI/SE
//	10 invoices spanning every aging bucket (paid, current, 1-30, 31-60, 90+)
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

type seedCustomer struct {
	name     string
	country  string
	currency string
}

type seedCustomerInvoice struct {
	customerName string
	currency     string
	total        float64
	invoiceDate  string
	dueDate      string
	description  string
	paid         bool   // if true, register a full payment after bookkeeping
	paymentDate  string // only used when paid=true
}

var customers = []seedCustomer{
	{name: e2ePrefix + "Acme Norge AS", country: "NO", currency: "NOK"},
	{name: e2ePrefix + "Berlin Digital GmbH", country: "DE", currency: "EUR"},
	{name: e2ePrefix + "Helsinki Holdings Oy", country: "FI", currency: "EUR"},
	{name: e2ePrefix + "Stockholm Stadsbolag AB", country: "SE", currency: "SEK"},
	{name: e2ePrefix + "Göteborg Maskin AB", country: "SE", currency: "SEK"},
}

// Customer invoices — dates relative to 2026-04-28 baseline.
// Aging buckets exercised: paid (×2), current (×3), 1–30 (×2), 31–60 (×2), 90+ (×1).
var customerInvoices = []seedCustomerInvoice{
	// paid (×2)
	{
		customerName: e2ePrefix + "Berlin Digital GmbH", currency: "EUR", total: 4500.00,
		invoiceDate: "2026-02-01", dueDate: "2026-03-03", description: e2ePrefix + "Q1 retainer",
		paid: true, paymentDate: "2026-02-28",
	},
	{
		customerName: e2ePrefix + "Helsinki Holdings Oy", currency: "EUR", total: 2200.00,
		invoiceDate: "2026-02-15", dueDate: "2026-03-17", description: e2ePrefix + "Discovery sprint",
		paid: true, paymentDate: "2026-03-10",
	},
	// current (×3) — due in the future
	{
		customerName: e2ePrefix + "Acme Norge AS", currency: "NOK", total: 18500.00,
		invoiceDate: "2026-04-15", dueDate: "2026-05-15", description: e2ePrefix + "Platform build",
	},
	{
		customerName: e2ePrefix + "Berlin Digital GmbH", currency: "EUR", total: 3200.00,
		invoiceDate: "2026-04-10", dueDate: "2026-05-10", description: e2ePrefix + "Q2 retainer",
	},
	{
		customerName: e2ePrefix + "Stockholm Stadsbolag AB", currency: "SEK", total: 45000.00,
		invoiceDate: "2026-04-05", dueDate: "2026-05-05", description: e2ePrefix + "Migration phase 2",
	},
	// overdue 1–30 (×2)
	{
		customerName: e2ePrefix + "Berlin Digital GmbH", currency: "EUR", total: 1850.00,
		invoiceDate: "2026-03-15", dueDate: "2026-04-14", description: e2ePrefix + "Audit follow-up",
	},
	{
		customerName: e2ePrefix + "Göteborg Maskin AB", currency: "SEK", total: 12500.00,
		invoiceDate: "2026-03-20", dueDate: "2026-04-19", description: e2ePrefix + "Maintenance window",
	},
	// overdue 31–60 (×2)
	{
		customerName: e2ePrefix + "Helsinki Holdings Oy", currency: "EUR", total: 3400.00,
		invoiceDate: "2026-02-15", dueDate: "2026-03-17", description: e2ePrefix + "Workshop fees",
	},
	{
		customerName: e2ePrefix + "Stockholm Stadsbolag AB", currency: "SEK", total: 22000.00,
		invoiceDate: "2026-02-10", dueDate: "2026-03-12", description: e2ePrefix + "Architecture review",
	},
	// overdue 90+ (×1) — collection-risk worst case
	{
		customerName: e2ePrefix + "Göteborg Maskin AB", currency: "SEK", total: 8500.00,
		invoiceDate: "2026-01-10", dueDate: "2026-01-25", description: e2ePrefix + "On-call support Dec",
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

	// AR side: customers + customer invoices.
	customerNumbers := make(map[string]int, len(customers))
	existingCustomers, err := client.ListCustomers(e2ePrefix)
	if err != nil {
		log.Error("list existing customers", "err", err)
		os.Exit(1)
	}
	for _, cu := range existingCustomers {
		customerNumbers[cu.Name] = cu.CustomerNumber
	}

	fmt.Println("\nSetting up customers...")
	for _, cu := range customers {
		if num, found := customerNumbers[cu.name]; found {
			if err := client.SetCustomerActive(num, true); err != nil {
				log.Error("reactivate customer", "name", cu.name, "err", err)
				os.Exit(1)
			}
			fmt.Printf("  ↺ %-40s → customer #%d (reactivated)\n", cu.name, num)
		} else {
			num, err := client.CreateCustomer(fortnox.CustomerCreate{
				Name:        cu.name,
				CountryCode: cu.country,
				Currency:    cu.currency,
			})
			if err != nil {
				log.Error("create customer", "name", cu.name, "err", err)
				os.Exit(1)
			}
			customerNumbers[cu.name] = num
			fmt.Printf("  ✓ %-40s → customer #%d (created)\n", cu.name, num)
		}
	}

	fmt.Println("\nCreating customer invoices...")
	for _, inv := range customerInvoices {
		cnum, ok := customerNumbers[inv.customerName]
		if !ok {
			log.Error("unknown customer", "name", inv.customerName)
			os.Exit(1)
		}
		docNumber, err := client.CreateCustomerInvoice(fortnox.CustomerInvoiceCreate{
			CustomerNumber: cnum,
			InvoiceDate:    inv.invoiceDate,
			DueDate:        inv.dueDate,
			Currency:       inv.currency,
			Description:    inv.description,
			Total:          inv.total,
		})
		if err != nil {
			log.Error("create customer invoice", "customer", inv.customerName, "err", err)
			os.Exit(1)
		}
		if err := client.BookkeepCustomerInvoice(docNumber); err != nil {
			log.Error("bookkeep customer invoice", "doc", docNumber, "err", err)
			os.Exit(1)
		}
		state := "unpaid"
		if inv.paid {
			if err := client.FullyPayCustomerInvoice(docNumber, inv.paymentDate); err != nil {
				log.Error("pay customer invoice", "doc", docNumber, "err", err)
				os.Exit(1)
			}
			state = "paid " + inv.paymentDate
		}
		fmt.Printf("  ✓ %-40s %s %9.2f  due %s  → invoice %s (%s)\n",
			inv.customerName, inv.currency, inv.total, inv.dueDate, docNumber, state)
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
