// Command e2e-teardown deactivates all E2E- suppliers in the Fortnox sandbox,
// hiding them and their invoices from operational views.
//
// Note: Fortnox does not allow deleting suppliers with associated invoices via
// the API, so teardown deactivates instead of deletes. The seed command
// reactivates them on the next run.
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

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))

	cfg, err := config.Load()
	if err != nil {
		log.Error("config", "err", err)
		os.Exit(1)
	}
	if !cfg.IsSandbox() {
		log.Error("refusing to run teardown against production — set FORTNOX_ENV=sandbox")
		os.Exit(1)
	}

	token, err := loadValidToken(cfg, log)
	if err != nil {
		log.Error("token", "err", err)
		os.Exit(1)
	}

	client := fortnox.NewClient(cfg.BaseURL(), token.AccessToken)

	suppliers, err := client.ListSuppliers("E2E-")
	if err != nil {
		log.Error("list suppliers", "err", err)
		os.Exit(1)
	}

	active := 0
	for _, s := range suppliers {
		if s.Active {
			active++
		}
	}
	if active == 0 {
		fmt.Println("No active E2E- suppliers found — nothing to tear down.")
		return
	}

	fmt.Printf("Deactivating %d active E2E- supplier(s)...\n", active)
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

	fmt.Println("\nTeardown complete.")
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
