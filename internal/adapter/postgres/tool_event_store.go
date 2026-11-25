package postgres

import (
	"context"
	"fmt"

	"github.com/mathiasb/cobalt-dingo/internal/adapter/postgres/pgstore"
	mcpserver "github.com/mathiasb/cobalt-dingo/internal/mcp"
)

var _ mcpserver.EventRecorder = (*ToolEventStore)(nil)

// ToolEventStore appends tool-call events to the tool_events table (#140).
// Append only: there is no read or delete method, and the table refuses
// updates and deletes by trigger.
type ToolEventStore struct{ s *Store }

// NewToolEventStore returns a ToolEventStore backed by s.
func NewToolEventStore(s *Store) *ToolEventStore { return &ToolEventStore{s: s} }

// Record implements mcpserver.EventRecorder.
func (t *ToolEventStore) Record(ctx context.Context, e mcpserver.Event) error {
	err := t.s.queries.InsertToolEvent(ctx, pgstore.InsertToolEventParams{
		At:         e.At,
		TenantID:   string(e.TenantID),
		Actor:      e.Actor,
		Tool:       e.Tool,
		Class:      string(e.Class),
		Status:     string(e.Status),
		DurationMs: e.Duration.Milliseconds(),
	})
	if err != nil {
		return fmt.Errorf("insert tool event: %w", err)
	}
	return nil
}
