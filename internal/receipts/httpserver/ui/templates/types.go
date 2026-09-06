package templates

import "time"

// TokenStatus describes the current Fortnox OAuth token state for a tenant.
type TokenStatus struct {
	Connected bool
	ExpiresAt time.Time
}

// DaysUntilExpiry returns full days until expiry, 0 if already expired.
func (s TokenStatus) DaysUntilExpiry() int {
	d := int(time.Until(s.ExpiresAt).Hours() / 24)
	if d < 0 {
		return 0
	}
	return d
}

// APIKey is a redacted view of an API key (plaintext never returned after creation).
type APIKey struct {
	ID         string
	Label      string
	CreatedAt  time.Time
	LastUsedAt *time.Time
}

// CreatedKey is returned once when a new key is created; Plaintext is never stored.
type CreatedKey struct {
	APIKey
	Plaintext string
}

// AuditEvent is a single audit log entry for display.
type AuditEvent struct {
	Timestamp  time.Time
	Operation  string
	Endpoint   string
	Method     string
	StatusCode int
	DurationMS int
}

// StatusData bundles all data for the status page.
type StatusData struct {
	Token   TokenStatus
	DB      bool
	Fortnox bool
}
