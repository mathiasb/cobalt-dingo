package mcp

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type funcRecorder func(context.Context, Event) error

func (f funcRecorder) Record(ctx context.Context, e Event) error { return f(ctx, e) }

func textHandler(text string) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return mcp.NewToolResultText(text), nil
	}
}

func TestWithEvents_ToolResultSurvivesAPanickingRecorder(t *testing.T) {
	rec := funcRecorder(func(context.Context, Event) error { panic("boom") })
	h := WithEvents("ap_summary", "t1", rec, textHandler("payload"))

	var res *mcp.CallToolResult
	require.NotPanics(t, func() {
		var err error
		res, err = h(context.Background(), mcp.CallToolRequest{})
		require.NoError(t, err)
	})

	require.NotNil(t, res)
	assert.Equal(t, "payload", res.Content[0].(mcp.TextContent).Text)
}

func TestWithEvents_CallReturnsWithinTheTimeoutWhenTheRecorderBlocks(t *testing.T) {
	old := recordTimeout
	recordTimeout = 50 * time.Millisecond
	t.Cleanup(func() { recordTimeout = old })

	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	rec := funcRecorder(func(context.Context, Event) error { <-release; return nil })
	h := WithEvents("ap_summary", "t1", rec, textHandler("payload"))

	done := make(chan *mcp.CallToolResult, 1)
	go func() {
		res, _ := h(context.Background(), mcp.CallToolRequest{})
		done <- res
	}()

	select {
	case res := <-done:
		assert.Equal(t, "payload", res.Content[0].(mcp.TextContent).Text)
	case <-time.After(2 * time.Second):
		t.Fatal("tool call still blocked by the recorder after 2s")
	}
}

func TestWithEvents_RecorderReceivesAContextThatIsNotCancelledWithTheRequest(t *testing.T) {
	var got error
	rec := funcRecorder(func(ctx context.Context, _ Event) error {
		got = ctx.Err()
		return errors.New("ignored")
	})
	h := WithEvents("ap_summary", "t1", rec, textHandler("x"))

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	_, _ = h(cancelled, mcp.CallToolRequest{})

	assert.NoError(t, got, "a client disconnect must not drop the event")
}
