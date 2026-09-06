// Package audit defines the Logger interface and Entry type for
// append-only structured audit logging throughout coo-agent.
package audit

import "time"

// Logger is the contract all audit loggers must satisfy.
// Other packages depend on this interface, never on a concrete type.
type Logger interface {
	Log(Entry) error
}

// Entry represents one audit log record.
type Entry struct {
	Timestamp  time.Time      `json:"ts"`
	Op         string         `json:"op"`
	Endpoint   string         `json:"endpoint"`
	Params     map[string]any `json:"params,omitempty"`
	Status     int            `json:"status,omitempty"`
	DurationMs int64          `json:"duration_ms,omitempty"`
	Agent      string         `json:"agent,omitempty"`
	Error      string         `json:"error,omitempty"`
}
