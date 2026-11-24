package mcp_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/mathiasb/cobalt-dingo/internal/domain"
	mcpserver "github.com/mathiasb/cobalt-dingo/internal/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type recorderStub struct {
	events []mcpserver.Event
	err    error
}

func (r *recorderStub) Record(_ context.Context, e mcpserver.Event) error {
	r.events = append(r.events, e)
	return r.err
}

func okHandler(text string) server.ToolHandlerFunc {
	return func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return mcp.NewToolResultText(text), nil
	}
}

func callWith(t *testing.T, h server.ToolHandlerFunc, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	req := mcp.CallToolRequest{}
	req.Params.Arguments = args
	res, err := h(context.Background(), req)
	require.NoError(t, err)
	return res
}

func TestWithEvents_RecordsExactlyOneEventPerCall(t *testing.T) {
	rec := &recorderStub{}
	h := mcpserver.WithEvents("ap_summary", domain.TenantID("t1"), rec, okHandler("fine"))

	callWith(t, h, nil)

	require.Len(t, rec.events, 1)
	e := rec.events[0]
	assert.Equal(t, "ap_summary", e.Tool)
	assert.Equal(t, domain.TenantID("t1"), e.TenantID)
	assert.Equal(t, mcpserver.StatusOK, e.Status)
	assert.Equal(t, mcpserver.ClassRead, e.Class)
	assert.False(t, e.At.IsZero())
	assert.GreaterOrEqual(t, int64(e.Duration), int64(0))
}

func TestWithEvents_RecordsErrorWhenHandlerReturnsToolError(t *testing.T) {
	rec := &recorderStub{}
	failing := func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return mcp.NewToolResultError("fetch invoices: boom"), nil
	}
	h := mcpserver.WithEvents("ap_overdue", "t1", rec, failing)

	callWith(t, h, nil)

	require.Len(t, rec.events, 1)
	assert.Equal(t, mcpserver.StatusError, rec.events[0].Status)
}

func TestWithEvents_RecordsErrorWhenHandlerReturnsGoError(t *testing.T) {
	rec := &recorderStub{}
	failing := func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return nil, errors.New("transport down")
	}
	h := mcpserver.WithEvents("ap_overdue", "t1", rec, failing)

	_, err := h(context.Background(), mcp.CallToolRequest{})

	require.Error(t, err, "the handler's own error must still reach the caller")
	require.Len(t, rec.events, 1)
	assert.Equal(t, mcpserver.StatusError, rec.events[0].Status)
}

// The write gate's error text reaches the tool result only as a string
// (handlers format errors with %v), so the wrapper matches on that text.
// This test pins the matching, not the gate; the gate has its own tests.
func TestWithEvents_RecordsRefusedWhenResultCarriesWriteBlockedText(t *testing.T) {
	rec := &recorderStub{}
	blocked := func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return mcp.NewToolResultError("create voucher: WRITE BLOCKED: this Fortnox client is read-only"), nil
	}
	h := mcpserver.WithEvents("create_voucher", "t1", rec, blocked)

	callWith(t, h, nil)

	require.Len(t, rec.events, 1)
	assert.Equal(t, mcpserver.StatusRefused, rec.events[0].Status)
}

func TestWithEvents_ToolResultIsUnchangedWhenRecorderFails(t *testing.T) {
	rec := &recorderStub{err: errors.New("db down")}
	h := mcpserver.WithEvents("ap_summary", "t1", rec, okHandler("payload"))

	res := callWith(t, h, nil)

	require.False(t, res.IsError)
	require.Len(t, res.Content, 1)
	assert.Equal(t, "payload", res.Content[0].(mcp.TextContent).Text)
}

func TestWithEvents_NilRecorderPassesTheCallThrough(t *testing.T) {
	h := mcpserver.WithEvents("ap_summary", "t1", nil, okHandler("payload"))

	res := callWith(t, h, nil)

	assert.False(t, res.IsError)
}

// Name states the mechanism: the Event struct's exported field set equals a
// reviewed list. It does NOT prove no sensitive value can reach a field (a
// string field could still be fed one); it makes adding a field a visible,
// reviewed change, and shows call arguments are not carried.
func TestEvent_FieldSetIsExactlyTheReviewedList(t *testing.T) {
	var names []string
	typ := reflect.TypeOf(mcpserver.Event{})
	for i := 0; i < typ.NumField(); i++ {
		names = append(names, typ.Field(i).Name)
	}
	sort.Strings(names)

	assert.Equal(t,
		[]string{"Actor", "At", "Class", "Duration", "Status", "TenantID", "Tool"},
		names,
		"adding a field to Event needs review: it must never carry amounts, counterparty names, document contents or tokens")
}

func TestWithEvents_RenderedEventContainsNeitherArgumentValue(t *testing.T) {
	rec := &recorderStub{}
	h := mcpserver.WithEvents("ap_supplier_history", "t1", rec, okHandler("x"))

	callWith(t, h, map[string]any{"supplier": "Acme AB", "amount": 12345})

	require.Len(t, rec.events, 1)
	rendered := fmt.Sprintf("%+v", rec.events[0])
	assert.NotContains(t, rendered, "Acme")
	assert.NotContains(t, rendered, "12345")
}

type companyStub struct{}

func (companyStub) Info(context.Context, domain.TenantID) (domain.Company, error) {
	return domain.Company{Name: "Test AB"}, nil
}

func TestDispatchTool_RecordsOneEventWhenDepsCarryARecorder(t *testing.T) {
	rec := &recorderStub{}
	deps := mcpserver.Deps{TenantID: "t1", CompanyInf: companyStub{}, Events: rec}

	h := mcpserver.DispatchTool("company_info", deps)
	require.NotNil(t, h)
	callWith(t, h, nil)

	require.Len(t, rec.events, 1)
	assert.Equal(t, "company_info", rec.events[0].Tool)
	assert.Equal(t, mcpserver.StatusOK, rec.events[0].Status)
}

func TestNewServer_RecordsOneEventForAToolsCallOverTheProtocol(t *testing.T) {
	rec := &recorderStub{}
	srv := mcpserver.NewServer(mcpserver.Deps{TenantID: "t1", CompanyInf: companyStub{}, Events: rec}, "test")

	resp := srv.HandleMessage(context.Background(), []byte(`{
		"jsonrpc": "2.0", "id": 2, "method": "tools/call",
		"params": {"name": "company_info", "arguments": {}}
	}`))
	require.NotNil(t, resp)

	require.Len(t, rec.events, 1)
	assert.Equal(t, "company_info", rec.events[0].Tool)
	assert.Equal(t, domain.TenantID("t1"), rec.events[0].TenantID)
}
