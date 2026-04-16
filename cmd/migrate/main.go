// Package main is the cobalt-dingo database migration runner.
// Usage:
//
//	go run ./cmd/migrate          — apply all pending migrations
//	go run ./cmd/migrate down 1   — roll back one migration
package main

import (
	"log/slog"
	"os"
	"strconv"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		log.Error("DATABASE_URL is not set")
		os.Exit(1)
	}

	m, err := migrate.New("file://migrations", dbURL)
	if err != nil {
		log.Error("create migrator", "err", err)
		os.Exit(1)
	}
	defer func() {
		srcErr, dbErr := m.Close()
		if srcErr != nil {
			log.Warn("close migrate source", "err", srcErr)
		}
		if dbErr != nil {
			log.Warn("close migrate db", "err", dbErr)
		}
	}()

	// Support: go run ./cmd/migrate down N
	if len(os.Args) >= 3 && os.Args[1] == "down" {
		n, err := strconv.Atoi(os.Args[2])
		if err != nil {
			log.Error("invalid step count", "arg", os.Args[2])
			os.Exit(1)
		}
		if err := m.Steps(-n); err != nil && err != migrate.ErrNoChange {
			log.Error("migrate down", "steps", n, "err", err)
			os.Exit(1)
		}
		log.Info("rolled back", "steps", n)
		return
	}

	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		log.Error("migrate up", "err", err)
		os.Exit(1)
	}
	log.Info("migrations applied")
}
