package ui

import (
	"context"

	"github.com/mathiasb/cobalt-dingo/internal/receipts/httpserver/ui/templates"
)

// TokenChecker reads and manages the Fortnox OAuth token for a tenant.
type TokenChecker interface {
	TokenStatus(ctx context.Context) (templates.TokenStatus, error)
	RefreshToken(ctx context.Context) error
	RevokeToken(ctx context.Context) error
}

// APIKeyStore manages API keys for a tenant.
type APIKeyStore interface {
	ListKeys(ctx context.Context) ([]templates.APIKey, error)
	CreateKey(ctx context.Context, label string) (templates.CreatedKey, error)
	RevokeKey(ctx context.Context, id string) error
}

// AuditReader reads the audit log for a tenant.
type AuditReader interface {
	ListEvents(ctx context.Context, page, pageSize int) ([]templates.AuditEvent, error)
}

// HealthChecker probes the health of backing services.
type HealthChecker interface {
	DBReachable(ctx context.Context) bool
	FortnoxReachable(ctx context.Context) bool
}
