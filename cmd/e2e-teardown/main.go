// Command e2e-teardown removes E2E- test data from the Fortnox sandbox across
// every ledger surface populated by e2e-seed: AP (suppliers + supplier
// invoices), AR (customers + customer invoices), projects, cost centers,
// and the asset register.
//
// Fortnox does not allow deleting suppliers, customers, projects, or
// bookkept invoices via the API, so teardown:
//   - cancels un-bookkept supplier and customer invoices (Fortnox exposes
//     no API to undo bookkeeping; bookkept invoices are permanent in the
//     ledger and are skipped with a note),
//   - deactivates suppliers, customers, and cost centers,
//   - sets projects to COMPLETED status,
//   - deletes the asset by integer Id (hard-delete for inactive assets,
//     auto-void for active ones).
//
// The seed command reactivates everything on the next run.
//
// Usage:
//
//	source .env && go run ./cmd/e2e-teardown
package main

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/mathiasb/cobalt-dingo/internal/config"
	"github.com/mathiasb/cobalt-dingo/internal/fortnox"
)

const e2ePrefix = "E2E-"

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))

	cfg, err := config.Load()
	if err != nil {
		log.Error("config", "err", err)
		os.Exit(1)
	}
	if cfg.Mode != config.ModeSandbox {
		log.Error("e2e-teardown only runs in sandbox mode", "current_mode", cfg.Mode)
		os.Exit(1)
	}
	fmt.Printf("\n  Mode: %s | Token: %s\n\n", cfg.Mode.Label(), cfg.Mode.TokenFile())

	token, err := loadValidToken(cfg, log)
	if err != nil {
		log.Error("token", "err", err)
		os.Exit(1)
	}

	client := fortnox.NewClient(cfg.BaseURL(), token.AccessToken, false)

	tearDownAP(client, log)
	tearDownAR(client, log)
	tearDownProjects(client, log)
	tearDownCostCenters(client, log)
	tearDownAssets(client, log)

	fmt.Println("\nTeardown complete.")
}

func tearDownAP(client *fortnox.Client, log *slog.Logger) {
	suppliers, err := client.ListSuppliers(e2ePrefix)
	if err != nil {
		log.Error("list suppliers", "err", err)
		os.Exit(1)
	}

	fmt.Println("Cancelling un-bookkept supplier invoices...")
	cancelled, bookkept := 0, 0
	for _, s := range suppliers {
		invoices, err := client.ListSupplierInvoicesBySupplier(s.SupplierNumber)
		if err != nil {
			log.Warn("list supplier invoices", "supplier", s.SupplierNumber, "err", err)
			continue
		}
		for _, inv := range invoices {
			if inv.Cancelled {
				continue
			}
			if inv.Booked {
				bookkept++
				continue
			}
			if err := client.CancelSupplierInvoice(inv.GivenNumber); err != nil {
				log.Warn("cancel supplier invoice", "given", inv.GivenNumber, "supplier", s.Name, "err", err)
				continue
			}
			fmt.Printf("  ✓ cancelled supplier invoice %s (%s)\n", inv.GivenNumber, s.Name)
			cancelled++
		}
	}
	if cancelled == 0 {
		fmt.Println("  (none cancellable)")
	}
	if bookkept > 0 {
		fmt.Printf("  (skipped %d bookkept invoice(s) — Fortnox has no unbookkeep API; they remain in the ledger)\n", bookkept)
	}

	active := 0
	for _, s := range suppliers {
		if s.Active {
			active++
		}
	}
	fmt.Printf("\nDeactivating %d active E2E- supplier(s)...\n", active)
	if active == 0 {
		fmt.Println("  (none)")
		return
	}
	for _, s := range suppliers {
		if !s.Active {
			continue
		}
		if err := client.DeactivateSupplier(s.SupplierNumber); err != nil {
			log.Warn("deactivate supplier", "supplier", s.SupplierNumber, "name", s.Name, "err", err)
			continue
		}
		fmt.Printf("  ✓ deactivated supplier #%d (%s)\n", s.SupplierNumber, s.Name)
	}
}

func tearDownAR(client *fortnox.Client, log *slog.Logger) {
	customers, err := client.ListCustomers(e2ePrefix)
	if err != nil {
		log.Error("list customers", "err", err)
		os.Exit(1)
	}

	fmt.Println("\nCancelling un-bookkept customer invoices...")
	cancelled, bookkept := 0, 0
	for _, cu := range customers {
		invoices, err := client.ListCustomerInvoicesByCustomer(cu.CustomerNumber)
		if err != nil {
			log.Warn("list customer invoices", "customer", cu.CustomerNumber, "err", err)
			continue
		}
		for _, inv := range invoices {
			if inv.Cancelled {
				continue
			}
			if inv.Booked {
				bookkept++
				continue
			}
			if err := client.CancelCustomerInvoice(inv.DocumentNumber); err != nil {
				log.Warn("cancel customer invoice", "doc", inv.DocumentNumber, "customer", cu.Name, "err", err)
				continue
			}
			fmt.Printf("  ✓ cancelled customer invoice %s (%s)\n", inv.DocumentNumber, cu.Name)
			cancelled++
		}
	}
	if cancelled == 0 {
		fmt.Println("  (none cancellable)")
	}
	if bookkept > 0 {
		fmt.Printf("  (skipped %d bookkept invoice(s) — Fortnox has no unbookkeep API; they remain in the ledger)\n", bookkept)
	}

	active := 0
	for _, cu := range customers {
		if cu.Active {
			active++
		}
	}
	fmt.Printf("\nDeactivating %d active E2E- customer(s)...\n", active)
	if active == 0 {
		fmt.Println("  (none)")
		return
	}
	for _, cu := range customers {
		if !cu.Active {
			continue
		}
		if err := client.SetCustomerActive(cu.CustomerNumber, false); err != nil {
			log.Warn("deactivate customer", "customer", cu.CustomerNumber, "name", cu.Name, "err", err)
			continue
		}
		fmt.Printf("  ✓ deactivated customer #%d (%s)\n", cu.CustomerNumber, cu.Name)
	}
}

func tearDownProjects(client *fortnox.Client, log *slog.Logger) {
	projects, err := client.ListProjectsByPrefix(e2ePrefix)
	if err != nil {
		log.Error("list projects", "err", err)
		os.Exit(1)
	}

	fmt.Println("\nClosing E2E- projects (status=COMPLETED)...")
	closed := 0
	for _, p := range projects {
		if p.Status == "COMPLETED" {
			continue
		}
		if err := client.SetProjectStatus(p.ProjectNumber, "COMPLETED"); err != nil {
			log.Warn("close project", "number", p.ProjectNumber, "desc", p.Description, "err", err)
			continue
		}
		fmt.Printf("  ✓ closed project %s (%s)\n", p.ProjectNumber, p.Description)
		closed++
	}
	if closed == 0 {
		fmt.Println("  (none open)")
	}
}

func tearDownCostCenters(client *fortnox.Client, log *slog.Logger) {
	costCenters, err := client.ListCostCentersByPrefix(e2ePrefix)
	if err != nil {
		log.Error("list cost centers", "err", err)
		os.Exit(1)
	}

	active := 0
	for _, cc := range costCenters {
		if cc.Active {
			active++
		}
	}
	fmt.Printf("\nDeactivating %d active E2E- cost center(s)...\n", active)
	if active == 0 {
		fmt.Println("  (none)")
		return
	}
	for _, cc := range costCenters {
		if !cc.Active {
			continue
		}
		if err := client.SetCostCenterActive(cc.Code, false); err != nil {
			log.Warn("deactivate cost center", "code", cc.Code, "err", err)
			continue
		}
		fmt.Printf("  ✓ deactivated cost center %s (%s)\n", cc.Code, cc.Description)
	}
}

func tearDownAssets(client *fortnox.Client, log *slog.Logger) {
	assets, err := client.ListAssetsByPrefix(e2ePrefix)
	if err != nil {
		log.Error("list assets", "err", err)
		os.Exit(1)
	}

	fmt.Printf("\nDeleting %d E2E- asset(s)...\n", len(assets))
	if len(assets) == 0 {
		fmt.Println("  (none)")
		return
	}
	for _, a := range assets {
		if err := client.DeleteAsset(a.ID); err != nil {
			log.Warn("delete asset", "id", a.ID, "number", a.Number, "err", err)
			continue
		}
		fmt.Printf("  ✓ deleted asset #%d %s (%s)\n", a.ID, a.Number, a.Description)
	}
}

func loadValidToken(cfg config.Fortnox, log *slog.Logger) (fortnox.Token, error) {
	tokenPath := cfg.Mode.TokenFile()
	t, err := fortnox.LoadToken(tokenPath)
	if err != nil {
		return fortnox.Token{}, fmt.Errorf("no saved token at %s — run fortnox-auth for mode %s: %w", tokenPath, cfg.Mode, err)
	}
	if t.Valid() {
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
