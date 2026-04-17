// Package postgres provides implementations of domain repository and token
// store ports backed by PostgreSQL. Generated query code lives in the pgstore
// sub-package; this package wraps it with domain translation and transaction
// management.
package postgres

import (
	"crypto/rand"
	"database/sql"
	"fmt"
	"time"

	"github.com/mathiasb/cobalt-dingo/internal/adapter/postgres/pgstore"
	"github.com/mathiasb/cobalt-dingo/internal/domain"

	_ "github.com/lib/pq" // postgres driver
)

// Store holds the shared database connection and generated query set.
// Construct one via NewStore; pass it to NewTokenStore, NewBatchRepo, NewTenantRepo.
type Store struct {
	db      *sql.DB
	queries *pgstore.Queries
}

// NewStore opens a postgres connection from connStr and pings it.
func NewStore(connStr string) (*Store, error) {
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		return nil, fmt.Errorf("postgres open: %w", err)
	}
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("postgres ping: %w", err)
	}
	return &Store{db: db, queries: pgstore.New(db)}, nil
}

// Close closes the underlying database pool.
func (s *Store) Close() error { return s.db.Close() }

// newUUID generates a random UUID v4 string without an external dependency.
func newUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10xx
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

// toDomainBatchStatus converts a database status string to domain.BatchStatus.
func toDomainBatchStatus(s string) domain.BatchStatus {
	return domain.BatchStatus(s)
}
