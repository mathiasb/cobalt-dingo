// Package main is the receipt-collector daemon binary.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"

	"github.com/joho/godotenv"
	"github.com/mathiasb/cobalt-dingo/internal/receipts/receipts"
)

func main() {
	_ = godotenv.Load()

	dryRun := true
	if v := os.Getenv("DRY_RUN"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			dryRun = b
		}
	}

	cfg, err := receipts.LoadConfig(receipts.DefaultConfigPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	sources, err := cfg.SourceList()
	if err != nil {
		log.Fatalf("credentials: %v", err)
	}
	dests := cfg.DestinationList()
	rules := cfg.RuleList()

	smtpCfg := receipts.SMTPConfig{
		Host:     os.Getenv("SMTP_HOST"),
		Port:     atoiDefault(os.Getenv("SMTP_PORT"), 465),
		Username: os.Getenv("SMTP_USER"),
		Password: os.Getenv("SMTP_PASSWORD"),
		From:     os.Getenv("SMTP_FROM"),
	}
	if !dryRun && smtpCfg.Host == "" {
		log.Fatal("SMTP_HOST måste sättas när DRY_RUN=false")
	}

	router := receipts.NewRouter(rules, dests)
	collector := receipts.NewCollector(sources, router, dryRun, smtpCfg)

	if dryRun {
		fmt.Println("=== DRY RUN – inga mail vidarebefordras ===")
	}

	results, err := collector.Run(context.Background())
	if err != nil {
		log.Fatalf("insamlingsfel: %v", err)
	}

	exitCode := 0
	for _, r := range results {
		fmt.Printf("\nKonto: %s\n", r.Account)
		fmt.Printf("  Hämtade:   %d\n", r.Processed)
		fmt.Printf("  Routade:   %d\n", r.Routed)
		fmt.Printf("  Omatchade: %d\n", r.UnmatchedCount)
		if len(r.Errors) > 0 {
			exitCode = 1
			fmt.Printf("  Fel:       %d\n", len(r.Errors))
			for _, e := range r.Errors {
				fmt.Printf("    - %v\n", e)
			}
		}
	}

	os.Exit(exitCode)
}

func atoiDefault(s string, def int) int {
	if n, err := strconv.Atoi(s); err == nil {
		return n
	}
	return def
}
