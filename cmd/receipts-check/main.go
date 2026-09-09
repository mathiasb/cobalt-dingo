// Package main is a read-only connectivity check for the configured mail
// accounts. It proves credentials work before the collector is pointed at real
// mail, and it cannot modify a mailbox: it opens every folder with EXAMINE
// (read-only), so the server will not set \Seen even if something below is
// wrong.
//
// It never prints a password. A failure is reported as the account name plus
// the server's error, which for a bad App Password is AUTHENTICATIONFAILED.
package main

import (
	"crypto/tls"
	"fmt"
	"os"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/joho/godotenv"

	"github.com/mathiasb/cobalt-dingo/internal/receipts/receipts"
)

func main() {
	_ = godotenv.Load()

	path := receipts.DefaultConfigPath
	if len(os.Args) > 1 {
		path = os.Args[1]
	}

	cfg, err := receipts.LoadConfig(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		os.Exit(1)
	}

	sources, err := cfg.SourceList()
	if err != nil {
		fmt.Fprintf(os.Stderr, "credentials: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Checking %d account(s) from %s — read-only, no flags changed.\n\n", len(sources), path)

	failed := 0
	for _, src := range sources {
		if err := checkAccount(src); err != nil {
			failed++
			fmt.Printf("  FAIL  %-24s %s\n", src.Name, src.Username)
			fmt.Printf("        %v\n", err)
			continue
		}
	}

	fmt.Println()
	if failed > 0 {
		fmt.Printf("%d of %d account(s) failed.\n", failed, len(sources))
		os.Exit(1)
	}
	fmt.Printf("All %d account(s) reachable.\n", len(sources))
}

// checkAccount dials, authenticates, and opens the folder READ-ONLY.
func checkAccount(src *receipts.Source) error {
	addr := fmt.Sprintf("%s:%d", src.Host, src.Port)

	var (
		c   *imapclient.Client
		err error
	)
	if src.TLS {
		c, err = imapclient.DialTLS(addr, &imapclient.Options{
			TLSConfig: &tls.Config{ServerName: src.Host, MinVersion: tls.VersionTLS12},
		})
	} else {
		c, err = imapclient.DialInsecure(addr, nil)
	}
	if err != nil {
		return fmt.Errorf("dial %s: %w", addr, err)
	}
	defer func() { _ = c.Close() }()

	if err := c.Login(src.Username, src.Password).Wait(); err != nil {
		return fmt.Errorf("login: %w", err)
	}

	folder := src.Folder
	if folder == "" {
		folder = "INBOX"
	}

	// ReadOnly = EXAMINE rather than SELECT. The whole point of this binary is
	// that running it cannot change anything in a real mailbox.
	data, err := c.Select(folder, &imap.SelectOptions{ReadOnly: true}).Wait()
	if err != nil {
		return fmt.Errorf("examine %s: %w", folder, err)
	}

	// The number the collector actually cares about: it processes UNSEEN.
	search, err := c.UIDSearch(&imap.SearchCriteria{
		NotFlag: []imap.Flag{imap.FlagSeen},
	}, nil).Wait()
	if err != nil {
		return fmt.Errorf("search unseen in %s: %w", folder, err)
	}

	fmt.Printf("  OK    %-24s %s\n", src.Name, src.Username)
	fmt.Printf("        %s: %d message(s) total, %d unseen (the collector's queue)\n",
		folder, data.NumMessages, len(search.AllUIDs()))

	_ = c.Logout().Wait()
	return nil
}
