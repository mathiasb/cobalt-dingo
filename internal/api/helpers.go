package api

import (
	"time"

	"github.com/mathiasb/coo-agent/internal/audit"
)

func audit_entry(op, endpoint string, status int, duration time.Duration) audit.Entry {
	return audit.Entry{
		Op:         op,
		Endpoint:   endpoint,
		Status:     status,
		DurationMs: duration.Milliseconds(),
	}
}
