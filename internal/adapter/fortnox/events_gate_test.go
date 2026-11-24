package fortnox_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mathiasb/cobalt-dingo/internal/fortnox"
	mcpserver "github.com/mathiasb/cobalt-dingo/internal/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type captureRecorder struct{ events []mcpserver.Event }

func (c *captureRecorder) Record(_ context.Context, e mcpserver.Event) error {
	c.events = append(c.events, e)
	return nil
}

// Couples the event log's "refused" matching to the REAL write-gate error, so
// rewording fortnox.ErrReadOnlyClient fails here instead of silently turning
// every refusal into an "error". Handlers format errors with %v, so the result
// text is built the same way.
func TestWithEvents_RecordsRefusedForTheRealReadOnlyClientError(t *testing.T) {
	rec := &captureRecorder{}
	gateErr := fmt.Errorf("%w: attempted POST /3/vouchers", fortnox.ErrReadOnlyClient)
	h := mcpserver.WithEvents("create_voucher", "t1", rec,
		func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return mcp.NewToolResultError(fmt.Sprintf("create voucher: %v", gateErr)), nil
		})

	_, err := h(context.Background(), mcp.CallToolRequest{})
	require.NoError(t, err)

	require.Len(t, rec.events, 1)
	assert.Equal(t, mcpserver.StatusRefused, rec.events[0].Status)
}
