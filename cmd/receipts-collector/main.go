// Package main is the receipt-collector daemon binary.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

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

	// Bound the run. Unbounded UNSEEN against a real backlog searches tens of
	// thousands of messages and fetches every body (#79), so both the window and
	// the cap are required rather than defaulted to "everything".
	since := time.Now().AddDate(0, -2, 0)
	if v := os.Getenv("RECEIPTS_SINCE"); v != "" {
		t, perr := time.Parse("2006-01-02", v)
		if perr != nil {
			log.Fatalf("RECEIPTS_SINCE: %v", perr)
		}
		since = t
	}
	scope, serr := receipts.NewScope(receipts.ScopeConfig{
		Since: since,
		Max:   atoiDefault(os.Getenv("RECEIPTS_MAX"), 50),
		// Gmail localises the Sent folder name. Overridable so a non-Swedish
		// account does not need a rebuild — the duplicate guard fails the run
		// if it cannot select this folder, so a wrong value is loud.
		SentFolder: os.Getenv("RECEIPTS_SENT_FOLDER"),
	})
	if serr != nil {
		log.Fatalf("scope: %v", serr)
	}
	fmt.Printf("scope: sedan %s, hogst %d meddelanden per korning, dubblettskydd mot %s\n",
		scope.Since.Format("2006-01-02"), scope.Max, scope.SentFolder)

	router := receipts.NewRouter(rules, dests)
	collector := receipts.NewCollector(sources, router, dryRun, smtpCfg).WithScope(scope)

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
		fmt.Printf("  Dubbletter (redan vidarebefordrade för hand): %d\n", r.DuplicateCount)
		for _, dm := range r.DuplicateMails {
			subj := dm.Subject
			if len(subj) > 58 {
				subj = subj[:58] + "..."
			}
			fmt.Printf("    skip %-34s %s\n", dm.From, subj)
		}
		for _, rm := range r.RoutedMails {
			subj := rm.Mail.Subject
			if len(subj) > 58 {
				subj = subj[:58] + "..."
			}
			fmt.Printf("    -> %-18s %-34s %s\n", rm.Destination, rm.Mail.From, subj)
		}
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
