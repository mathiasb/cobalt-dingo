package mcp

import (
	"context"
	"fmt"
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
// A nil recorder passes the call straight through. A handler that panics
// records no event: there is no defer, so a panicking tool is invisible to the
// log. Known and accepted for now.
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
		record(ctx, rec, e)
		return res, err
	}
}

// recordTimeout bounds how long a tool call waits for the recorder. A variable
// so a test can shorten it.
var recordTimeout = 2 * time.Second

// record hands e to rec without letting the recorder harm the call: it runs on
// its own goroutine, a panic is recovered, a recorder that does not return is
// abandoned after recordTimeout, and the context is detached from the request
// so a client disconnect does not drop the event. A recorder that never
// returns leaks its goroutine; that is accepted over blocking the tool call.
func record(ctx context.Context, rec EventRecorder, e Event) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), recordTimeout)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				done <- fmt.Errorf("recorder panicked: %v", r)
			}
		}()
		done <- rec.Record(ctx, e)
	}()

	select {
	case err := <-done:
		if err != nil {
			slog.Warn("event log write failed", "tool", e.Tool, "error", err)
		}
	case <-ctx.Done():
		slog.Warn("event log write abandoned", "tool", e.Tool, "reason", ctx.Err())
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
