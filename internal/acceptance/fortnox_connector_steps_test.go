package acceptance_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"

	"github.com/cucumber/godog"
	"github.com/mathiasb/cobalt-dingo/internal/fortnox"
	"github.com/mathiasb/cobalt-dingo/internal/invoice"
)

type fortnoxConnectorCtx struct {
	stub    *httptest.Server
	results []invoice.SupplierInvoice
}

var fcCtx fortnoxConnectorCtx

func aFortnoxAPIStubReturningTheseUnpaidSupplierInvoices(table *godog.Table) error {
	fcCtx = fortnoxConnectorCtx{}

	var rows []fortnox.SupplierInvoiceRow
	for _, row := range table.Rows[1:] {
		num, err := strconv.Atoi(row.Cells[0].Value)
		if err != nil {
			return fmt.Errorf("parse InvoiceNumber: %w", err)
		}
		total, err := strconv.ParseFloat(row.Cells[2].Value, 64)
		if err != nil {
			return fmt.Errorf("parse TotalInvoiceCurrency: %w", err)
		}
		rows = append(rows, fortnox.SupplierInvoiceRow{
			InvoiceNumber:        num,
			Currency:             row.Cells[1].Value,
			TotalInvoiceCurrency: total,
			DueDate:              row.Cells[3].Value,
		})
	}

	fcCtx.stub = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(fortnox.SupplierInvoicesResponse{SupplierInvoices: rows})
	}))
	return nil
}

func theFortnoxConnectorFetchesUnpaidInvoices() error {
	client := fortnox.NewClient(fcCtx.stub.URL, "test-token")
	invoices, err := client.UnpaidSupplierInvoices()
	if err != nil {
		return fmt.Errorf("fetch unpaid invoices: %w", err)
	}
	fcCtx.results = invoices
	return nil
}

func nInvoicesAreReturned(n int) error {
	if len(fcCtx.results) != n {
		return fmt.Errorf("expected %d invoices, got %d", n, len(fcCtx.results))
	}
	return nil
}

func invoiceHasCurrencyAndTotal(num int, currency string, total float64) error {
	for _, inv := range fcCtx.results {
		if inv.InvoiceNumber == num {
			if inv.Currency != currency {
				return fmt.Errorf("invoice %d: expected currency %s, got %s", num, currency, inv.Currency)
			}
			if inv.Total != total {
				return fmt.Errorf("invoice %d: expected total %.2f, got %.2f", num, total, inv.Total)
			}
			return nil
		}
	}
	return fmt.Errorf("invoice %d not found in results", num)
}

func initializeFortnoxConnectorSteps(sc *godog.ScenarioContext) {
	sc.After(func(ctx context.Context, _ *godog.Scenario, _ error) (context.Context, error) {
		if fcCtx.stub != nil {
			fcCtx.stub.Close()
		}
		return ctx, nil
	})
	sc.Step(`^a Fortnox API stub returning these unpaid supplier invoices:$`, aFortnoxAPIStubReturningTheseUnpaidSupplierInvoices)
	sc.Step(`^the Fortnox connector fetches unpaid invoices$`, theFortnoxConnectorFetchesUnpaidInvoices)
	sc.Step(`^(\d+) invoices are returned$`, nInvoicesAreReturned)
	sc.Step(`^invoice (\d+) has currency (\w+) and total (\d+\.\d+)$`, invoiceHasCurrencyAndTotal)
}
