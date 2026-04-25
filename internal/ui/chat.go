package ui

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mathiasb/cobalt-dingo/internal/config"
	mcpserver "github.com/mathiasb/cobalt-dingo/internal/mcp"
)

// ChatHandler handles the /chat page and message API.
type ChatHandler struct {
	deps      mcpserver.Deps
	claudeCfg config.Claude
	log       *slog.Logger
}

// NewChatHandler constructs a ChatHandler wired to the given MCP deps.
func NewChatHandler(deps mcpserver.Deps, claudeCfg config.Claude, log *slog.Logger) *ChatHandler {
	return &ChatHandler{deps: deps, claudeCfg: claudeCfg, log: log}
}

// PageHandler serves GET /chat — renders the chat template.
func (h *ChatHandler) PageHandler(w http.ResponseWriter, r *http.Request) {
	render(w, r, ChatPage())
}

// chatMessage mirrors the Claude API message format.
type chatMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"` // string | []contentBlock
}

// contentBlock is a typed content block (text or tool_result).
type contentBlock struct {
	Type      string `json:"type"`
	Text      string `json:"text,omitempty"`
	ToolUseID string `json:"tool_use_id,omitempty"`
	Content   string `json:"content,omitempty"`
}

// toolDef is the simplified tool definition sent to Claude (name + description only).
type toolDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"input_schema"`
}

// claudeRequest is the body sent to the Claude Messages API.
type claudeRequest struct {
	Model     string        `json:"model"`
	MaxTokens int           `json:"max_tokens"`
	System    string        `json:"system"`
	Messages  []chatMessage `json:"messages"`
	Tools     []toolDef     `json:"tools"`
}

// claudeResponse is the response envelope from the Claude Messages API.
type claudeResponse struct {
	Content []struct {
		Type  string `json:"type"`
		Text  string `json:"text,omitempty"`
		ID    string `json:"id,omitempty"`
		Name  string `json:"name,omitempty"`
		Input any    `json:"input,omitempty"`
	} `json:"content"`
	StopReason string `json:"stop_reason"`
}

// toolSchemas returns the simplified tool definitions for all 38 MCP tools.
func toolSchemas() []toolDef {
	empty := map[string]any{"type": "object", "properties": map[string]any{}}
	return []toolDef{
		{Name: "ap_summary", Description: "Summarise unpaid supplier invoices: count and totals grouped by currency, plus the oldest due date.", InputSchema: empty},
		{Name: "ap_overdue", Description: "List overdue supplier invoices, sorted by days overdue descending.", InputSchema: empty},
		{Name: "ap_by_supplier", Description: "Group unpaid invoices by supplier, sorted by total amount descending.", InputSchema: empty},
		{Name: "ap_by_currency", Description: "Group unpaid invoices by currency with counts and totals.", InputSchema: empty},
		{Name: "ap_aging", Description: "Aging report for unpaid supplier invoices bucketed into Current, 1-30, 31-60, 61-90 and 90+ days.", InputSchema: empty},
		{Name: "ap_supplier_history", Description: "Supplier master data plus all outstanding invoices for a given supplier. Requires supplier_number.", InputSchema: empty},
		{Name: "ap_invoice_detail", Description: "Detail and payment history for a single supplier invoice. Requires invoice_number.", InputSchema: empty},
		{Name: "ar_summary", Description: "Summarise unpaid customer invoices: count and totals grouped by currency, plus the oldest due date.", InputSchema: empty},
		{Name: "ar_overdue", Description: "List overdue customer invoices, sorted by days overdue descending.", InputSchema: empty},
		{Name: "ar_by_customer", Description: "Group unpaid customer invoices by customer, sorted by total amount descending.", InputSchema: empty},
		{Name: "ar_aging", Description: "Aging report for unpaid customer invoices bucketed into Current, 1-30, 31-60, 61-90 and 90+ days.", InputSchema: empty},
		{Name: "ar_customer_history", Description: "Customer master data plus all outstanding invoices for a given customer. Requires customer_number.", InputSchema: empty},
		{Name: "ar_invoice_detail", Description: "Detail and payment history for a single customer invoice. Requires invoice_number.", InputSchema: empty},
		{Name: "ar_unpaid_report", Description: "Detailed overdue analysis with customer contact info (email, phone) for each overdue invoice.", InputSchema: empty},
		{Name: "gl_chart_of_accounts", Description: "Return all GL accounts for a financial year (defaults to the latest year).", InputSchema: empty},
		{Name: "gl_account_balance", Description: "Period balances for a range of GL accounts. Requires account_from and account_to.", InputSchema: empty},
		{Name: "gl_account_activity", Description: "All voucher rows posted to a specific GL account within a date range. Requires account_num, from_date, to_date.", InputSchema: empty},
		{Name: "gl_vouchers", Description: "All vouchers (journal entries) posted within a date range. Requires from_date and to_date.", InputSchema: empty},
		{Name: "gl_voucher_detail", Description: "Full detail for a single voucher. Requires series and number.", InputSchema: empty},
		{Name: "gl_predefined_accounts", Description: "Return the system-defined account mappings (e.g. VAT, retained earnings).", InputSchema: empty},
		{Name: "gl_financial_years", Description: "List all financial years configured in Fortnox.", InputSchema: empty},
		{Name: "project_list", Description: "List all projects in Fortnox.", InputSchema: empty},
		{Name: "project_transactions", Description: "All voucher rows posted to a project within an optional date range. Requires project_id.", InputSchema: empty},
		{Name: "project_profitability", Description: "Calculate project profitability by summing debit (cost) vs credit (revenue) rows. Requires project_id.", InputSchema: empty},
		{Name: "costcenter_list", Description: "List all cost centers defined in Fortnox.", InputSchema: empty},
		{Name: "costcenter_transactions", Description: "All voucher rows posted to a cost center within an optional date range. Requires code.", InputSchema: empty},
		{Name: "costcenter_analysis", Description: "Group all cost center transactions by account number, showing debit, credit and net. Requires code.", InputSchema: empty},
		{Name: "asset_list", Description: "List all fixed assets in the asset register.", InputSchema: empty},
		{Name: "asset_detail", Description: "Full detail for a single fixed asset. Requires asset_id.", InputSchema: empty},
		{Name: "cash_flow_forecast", Description: "Forecast cash flow over a horizon: inflows from unpaid AR, outflows from unpaid AP, both filtered to invoices due within the horizon.", InputSchema: empty},
		{Name: "expense_analysis", Description: "Group debit amounts for BAS expense accounts (4000-7999) by account for a date range. Requires from_date and to_date.", InputSchema: empty},
		{Name: "period_comparison", Description: "Compare revenue and COGS side-by-side for two date ranges. Requires period1_from, period1_to, period2_from, period2_to.", InputSchema: empty},
		{Name: "yearly_comparison", Description: "Compare account balances for two financial years side-by-side. Requires year1 and year2.", InputSchema: empty},
		{Name: "gross_margin_trend", Description: "Monthly gross margin trend: revenue, COGS and margin % for each of the last N months.", InputSchema: empty},
		{Name: "top_customers", Description: "Rank customers by unpaid AR invoice totals for a date range. Requires from_date and to_date.", InputSchema: empty},
		{Name: "top_suppliers", Description: "Rank suppliers by unpaid AP invoice totals for a date range. Requires from_date and to_date.", InputSchema: empty},
		{Name: "sales_vs_purchases", Description: "Compare revenue and COGS for a period, returning both totals and the net. Requires from_date and to_date.", InputSchema: empty},
		{Name: "company_info", Description: "Return the company profile from the ERP.", InputSchema: empty},
	}
}

// dispatchTool routes a tool name + raw JSON args to the matching MCP handler.
func (h *ChatHandler) dispatchTool(ctx context.Context, name string, rawInput any) (string, error) {
	// Build a mcp.CallToolRequest from rawInput (which Claude returns as map[string]any).
	args := map[string]any{}
	if rawInput != nil {
		if m, ok := rawInput.(map[string]any); ok {
			args = m
		}
	}

	req := mcp.CallToolRequest{}
	req.Params.Name = name
	req.Params.Arguments = args

	// Dispatch to the appropriate handler function by name.
	handler := mcpserver.DispatchTool(name, h.deps)
	if handler == nil {
		return fmt.Sprintf(`{"error": "unknown tool %q"}`, name), nil
	}

	result, err := handler(ctx, req)
	if err != nil {
		return fmt.Sprintf(`{"error": %q}`, err.Error()), nil
	}
	if result == nil {
		return `{"error": "nil result"}`, nil
	}

	// Extract text content from the tool result.
	for _, c := range result.Content {
		if tc, ok := c.(mcp.TextContent); ok {
			return tc.Text, nil
		}
	}
	return `{}`, nil
}

// callClaude posts a request to the Claude Messages API and returns the response.
func (h *ChatHandler) callClaude(ctx context.Context, req claudeRequest) (*claudeResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.anthropic.com/v1/messages", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", h.claudeCfg.APIKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("claude api error %d: %s", resp.StatusCode, respBody)
	}

	var cr claudeResponse
	if err := json.Unmarshal(respBody, &cr); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &cr, nil
}

// MessageHandler handles POST /chat — processes the user message and streams back via SSE.
func (h *ChatHandler) MessageHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var body struct {
		Messages []chatMessage `json:"messages"`
		Message  string        `json:"message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}

	// Build message history: existing messages + new user message.
	messages := append(body.Messages, chatMessage{
		Role:    "user",
		Content: body.Message,
	})

	claudeReq := claudeRequest{
		Model:     h.claudeCfg.Model,
		MaxTokens: 4096,
		System:    "You are a financial analyst for a Swedish company using Fortnox ERP. Use your tools to answer questions about accounts payable (AP), accounts receivable (AR), general ledger (GL), projects, cost centers, and assets. Always use tools to fetch live data before answering. Respond in the same language as the user's question.",
		Messages:  messages,
		Tools:     toolSchemas(),
	}

	// Set SSE headers before any writes.
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher, canFlush := w.(http.Flusher)

	sseWrite := func(data string) {
		_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
		if canFlush {
			flusher.Flush()
		}
	}

	// Agentic loop: call Claude, execute tools, repeat until stop_reason != "tool_use".
	const maxTurns = 5
	for turn := 0; turn < maxTurns; turn++ {
		cr, err := h.callClaude(ctx, claudeReq)
		if err != nil {
			h.log.Error("claude api call failed", "turn", turn, "err", err)
			sseWrite(fmt.Sprintf("[error: %v]", err))
			return
		}

		if cr.StopReason != "tool_use" {
			// Extract and stream final text response.
			for _, block := range cr.Content {
				if block.Type == "text" && block.Text != "" {
					sseWrite(block.Text)
				}
			}
			return
		}

		// Execute tool calls and build the next turn.
		assistantContent := make([]map[string]any, 0, len(cr.Content))
		for _, block := range cr.Content {
			b := map[string]any{"type": block.Type}
			switch block.Type {
			case "text":
				b["text"] = block.Text
			case "tool_use":
				b["id"] = block.ID
				b["name"] = block.Name
				b["input"] = block.Input
			}
			assistantContent = append(assistantContent, b)
		}

		// Append assistant turn with tool_use blocks.
		claudeReq.Messages = append(claudeReq.Messages, chatMessage{
			Role:    "assistant",
			Content: assistantContent,
		})

		// Execute each tool and collect results.
		toolResults := make([]contentBlock, 0, len(cr.Content))
		for _, block := range cr.Content {
			if block.Type != "tool_use" {
				continue
			}
			h.log.Info("executing tool", "name", block.Name)
			result, toolErr := h.dispatchTool(ctx, block.Name, block.Input)
			if toolErr != nil {
				result = fmt.Sprintf(`{"error": %q}`, toolErr.Error())
			}
			toolResults = append(toolResults, contentBlock{
				Type:      "tool_result",
				ToolUseID: block.ID,
				Content:   result,
			})
		}

		// Append user turn with tool results.
		claudeReq.Messages = append(claudeReq.Messages, chatMessage{
			Role:    "user",
			Content: toolResults,
		})
	}

	// Exceeded max turns — return whatever we have.
	sseWrite("[max tool turns reached]")
}
