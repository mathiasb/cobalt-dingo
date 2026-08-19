// Command e2e-token moves the sandbox Fortnox OAuth token between postgres and
// the local files the E2E suite reads, without any cluster access.
//
// It replaces two CI steps that shelled out to `kubectl get secret` +
// `kubectl exec -n databases <pg-pod> -- psql`. That worked while the runner
// ran on the host with a cluster-admin kubeconfig; since the runner moved into
// a pod with its own ServiceAccount (infra 949b333) it has neither `get
// secrets` in cobalt-dingo nor `pods/exec` in databases, so both steps failed
// Forbidden. Granting them would have handed every CI job in every
// dispatch-allowlisted repo an exec shell on the shared postgres pod.
//
// Instead the connection string arrives as a Gitea Actions secret
// (DATABASE_URL) and this program talks to postgres directly — reachable from
// the runner, which is hostNetwork:true on koala.
//
// Token values are never logged: the fetch summary prints lengths, and persist
// prints only the affected row count.
package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	_ "github.com/lib/pq"
)

// sandboxTenant matches the sandbox tenant rows regardless of tenant prefix,
// mirroring the `tenant_id LIKE '%:sandbox'` the CI steps used.
const sandboxTenant = "%:sandbox"

const (
	tokenFile = ".fortnox-tokens-sandbox.json"
	oldRTFile = ".old-refresh"
)

// token is the on-disk shape the E2E suite loads. expires_at carries the real
// expiry (not a forced-stale value) so the suite can use the access token
// directly instead of refreshing on first use and racing the running pod,
// which holds the same refresh token.
type token struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresAt    string `json:"expires_at"`
}

// shouldWriteBack reports whether a rotated token needs persisting. Fortnox
// rotates the refresh token on use, so an unchanged value means the run never
// refreshed and writing back would be a no-op UPDATE against the same row.
func shouldWriteBack(oldRefresh, newRefresh string) bool {
	newRefresh = strings.TrimSpace(newRefresh)
	if newRefresh == "" {
		return false
	}
	return newRefresh != strings.TrimSpace(oldRefresh)
}

func main() {
	if len(os.Args) < 2 {
		fatal(errors.New("usage: e2e-token fetch|persist"))
	}

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		fatal(errors.New("DATABASE_URL is not set — expected the Gitea Actions secret"))
	}

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		fatal(fmt.Errorf("open postgres: %w", err))
	}
	defer func() { _ = db.Close() }()

	switch os.Args[1] {
	case "fetch":
		err = fetch(db)
	case "persist":
		err = persist(db)
	default:
		err = fmt.Errorf("unknown subcommand %q (want fetch|persist)", os.Args[1])
	}
	if err != nil {
		fatal(err)
	}
}

// fetch writes the newest sandbox token to tokenFile, and its refresh token to
// oldRTFile so persist can compare against it (optimistic lock).
func fetch(db *sql.DB) error {
	var (
		tok       token
		expiresAt time.Time
	)
	err := db.QueryRow(
		`SELECT access_token, refresh_token, expires_at
		   FROM fortnox_tokens
		  WHERE tenant_id LIKE $1
		  ORDER BY expires_at DESC
		  LIMIT 1`, sandboxTenant,
	).Scan(&tok.AccessToken, &tok.RefreshToken, &expiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("no sandbox token row (tenant_id LIKE %q) — the sandbox tenant needs connecting once by hand", sandboxTenant)
	}
	if err != nil {
		return fmt.Errorf("query sandbox token: %w", err)
	}

	tok.TokenType = "bearer"
	tok.ExpiresAt = expiresAt.UTC().Format("2006-01-02T15:04:05Z")

	blob, err := json.Marshal(tok)
	if err != nil {
		return fmt.Errorf("marshal token: %w", err)
	}
	if err := os.WriteFile(tokenFile, blob, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", tokenFile, err)
	}
	if err := os.WriteFile(oldRTFile, []byte(tok.RefreshToken), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", oldRTFile, err)
	}

	fmt.Printf("✓ sandbox token fetched (expires_at=%s, access_token=%d bytes)\n",
		tok.ExpiresAt, len(tok.AccessToken))
	return nil
}

// persist writes a rotated token back, guarded by a compare-and-set on the
// refresh token that was read at fetch time: if the running pod refreshed
// concurrently, the WHERE clause matches nothing and this leaves that newer
// token alone rather than clobbering it.
func persist(db *sql.DB) error {
	blob, err := os.ReadFile(tokenFile)
	if errors.Is(err, os.ErrNotExist) {
		fmt.Println("no token file — skip")
		return nil
	}
	if err != nil {
		return fmt.Errorf("read %s: %w", tokenFile, err)
	}

	var tok token
	if err := json.Unmarshal(blob, &tok); err != nil {
		return fmt.Errorf("parse %s: %w", tokenFile, err)
	}

	oldRT, err := os.ReadFile(oldRTFile)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read %s: %w", oldRTFile, err)
	}

	if !shouldWriteBack(string(oldRT), tok.RefreshToken) {
		fmt.Println("sandbox token unchanged — no write-back needed")
		return nil
	}

	if err := writeBack(db, tok, strings.TrimSpace(string(oldRT))); err != nil {
		return err
	}
	return nil
}

// execer is the slice of *sql.DB the write-back needs, so the compare-and-set
// can be tested without a database. The CAS is the part that can lose a token;
// before cobalt-dingo#41 it had no test, while the commit shipping it claimed
// otherwise.
type execer interface {
	Exec(query string, args ...any) (sql.Result, error)
}

// writeBack persists a rotated token, guarded by a compare-and-set on the
// refresh token read at fetch time.
//
// Zero rows affected is a SUCCESS: it means the running pod refreshed
// concurrently and holds a newer token, which must not be clobbered.
//
// A database error is a FAILURE and is returned. It used to be printed and
// swallowed, so the command exited 0, CI went green, and the rotated token was
// simply gone — the next run then needed a manual re-auth with no clue why.
// That is the same silent-success class dispatch#33/#41 are about, on the money
// path: a job whose failure mode is indistinguishable from success.
func writeBack(db execer, tok token, oldRefresh string) error {
	res, err := db.Exec(
		`UPDATE fortnox_tokens
		    SET access_token = $1, refresh_token = $2, expires_at = $3, updated_at = NOW()
		  WHERE tenant_id LIKE $4
		    AND refresh_token = $5`,
		tok.AccessToken, tok.RefreshToken, tok.ExpiresAt, sandboxTenant, oldRefresh,
	)
	if err != nil {
		return fmt.Errorf("persist rotated sandbox token (the rotated token is now lost; the next run needs re-auth): %w", err)
	}

	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if n == 0 {
		fmt.Println("⚠ CAS matched no row — the running pod refreshed concurrently; leaving its token in place")
		return nil
	}
	fmt.Printf("✓ persisted rotated sandbox token back to postgres (%d row, CAS on prior refresh_token)\n", n)
	return nil
}

func fatal(err error) {
	fmt.Fprintf(os.Stderr, "e2e-token: %v\n", err)
	os.Exit(1)
}
