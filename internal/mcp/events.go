package mcp

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/mathiasb/cobalt-dingo/internal/domain"
)

// Class says whether a tool reads or writes. Every tool registered today is a
// read; the field exists so a later write verb is distinguishable in the log.
type Class string

// Tool classes.
const (
	ClassRead  Class = "read"
	ClassWrite Class = "write"
)

// Status is the outcome of one tool call.
type Status string

// Call outcomes.
const (
	StatusOK      Status = "ok"
	StatusError   Status = "error"
	StatusRefused Status = "refused"
)

// writeBlockedMarker is the text the Fortnox client's write gate puts in its
// error (fortnox.ErrReadOnlyClient). Handlers format errors with %v, so the
// sentinel is lost by the time the result reaches the wrapper and only the
// text survives. Matching text is a known weakness; it is pinned by a test.
const writeBlockedMarker = "WRITE BLOCKED"

// Event is one tool call, for the bet #139 evidence trail. The exported field
// set is pinned by a test: it must never carry amounts, counterparty names,
// document contents, call arguments or tokens.
type Event struct {
	At       time.Time
	TenantID domain.TenantID
	Actor    string // internal user id; empty when the call has none (cron, tests)
	Tool     string
	Class    Class
	Status   Status
	Duration time.Duration
}

// EventRecorder persists events. Implementations must be append-only.
type EventRecorder interface {
	Record(ctx context.Context, e Event) error
}

// WithEvents wraps next so each call records exactly one Event. Recording is
// best effort: a failing recorder is logged and never changes the tool result.
// A nil recorder passes the call straight through.
func WithEvents(tool string, tenant domain.TenantID, rec EventRecorder, next server.ToolHandlerFunc) server.ToolHandlerFunc {
	if rec == nil {
		return next
	}
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		start := time.Now()
		res, err := next(ctx, req)

		e := Event{
			At:       start,
			TenantID: tenant,
			Tool:     tool,
			Class:    ClassRead,
			Status:   statusOf(res, err),
			Duration: time.Since(start),
		}
		if rerr := rec.Record(ctx, e); rerr != nil {
			slog.Warn("event log write failed", "tool", tool, "error", rerr)
		}
		return res, err
	}
}

func statusOf(res *mcp.CallToolResult, err error) Status {
	if err != nil {
		return StatusError
	}
	if res == nil || !res.IsError {
		return StatusOK
	}
	for _, c := range res.Content {
		if t, ok := c.(mcp.TextContent); ok && strings.Contains(t.Text, writeBlockedMarker) {
			return StatusRefused
		}
	}
	return StatusError
}
