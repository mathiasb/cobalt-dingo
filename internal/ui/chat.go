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

type ChatHandler struct {
	deps   mcpserver.Deps
	llmCfg config.LLM
	mode   config.Mode
	log    *slog.Logger
}

func NewChatHandler(deps mcpserver.Deps, llmCfg config.LLM, mode config.Mode, log *slog.Logger) *ChatHandler {
	return &ChatHandler{deps: deps, llmCfg: llmCfg, mode: mode, log: log}
}

// PageHandler serves GET /chat — renders the chat template.
func (h *ChatHandler) PageHandler(w http.ResponseWriter, r *http.Request) {
	render(w, r, ChatPage(h.mode, h.llmCfg))
}

type llmMessage struct {
	Role       string     `json:"role"`
	Content    any        `json:"content,omitempty"`
	ToolCalls  []toolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	Name       string     `json:"name,omitempty"`
}

type toolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function toolCallFunc `json:"function"`
}

type toolCallFunc struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type llmRequest struct {
	Model     string       `json:"model"`
	Messages  []llmMessage `json:"messages"`
	Tools     []llmTool    `json:"tools,omitempty"`
	MaxTokens int          `json:"max_tokens"`
}

type llmTool struct {
	Type     string      `json:"type"`
	Function llmFunction `json:"function"`
}

type llmFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

type llmChoice struct {
	Message      llmMessage `json:"message"`
	FinishReason string     `json:"finish_reason"`
}

type llmResponse struct {
	Choices []llmChoice `json:"choices"`
}

func toolSchemas() []llmTool {
	empty := map[string]any{"type": "object", "properties": map[string]any{}}
	t := func(name, desc string) llmTool {
		return llmTool{
			Type: "function",
			Function: llmFunction{
				Name:        name,
				Description: desc,
				Parameters:  empty,
			},
		}
	}
	return []llmTool{
		t("ap_summary", "Summarise unpaid supplier invoices: count and totals grouped by currency, plus the oldest due date."),
		t("ap_overdue", "List overdue supplier invoices, sorted by days overdue descending."),
		t("ap_by_supplier", "Group unpaid invoices by supplier, sorted by total amount descending."),
		t("ap_by_currency", "Group unpaid invoices by currency with counts and totals."),
		t("ap_aging", "Aging report for unpaid supplier invoices bucketed into Current, 1-30, 31-60, 61-90 and 90+ days."),
		t("ap_supplier_history", "Supplier master data plus all outstanding invoices for a given supplier. Requires supplier_number."),
		t("ap_invoice_detail", "Detail and payment history for a single supplier invoice. Requires invoice_number."),
		t("ar_summary", "Summarise unpaid customer invoices: count and totals grouped by currency, plus the oldest due date."),
		t("ar_overdue", "List overdue customer invoices, sorted by days overdue descending."),
		t("ar_by_customer", "Group unpaid customer invoices by customer, sorted by total amount descending."),
		t("ar_aging", "Aging report for unpaid customer invoices bucketed into Current, 1-30, 31-60, 61-90 and 90+ days."),
		t("ar_customer_history", "Customer master data plus all outstanding invoices for a given customer. Requires customer_number."),
		t("ar_invoice_detail", "Detail and payment history for a single customer invoice. Requires invoice_number."),
		t("ar_unpaid_report", "Detailed overdue analysis with customer contact info (email, phone) for each overdue invoice."),
		t("gl_chart_of_accounts", "Return all GL accounts for a financial year (defaults to the latest year)."),
		t("gl_account_balance", "Period balances for a range of GL accounts. Requires account_from and account_to."),
		t("gl_account_activity", "All voucher rows posted to a specific GL account within a date range. Requires account_num, from_date, to_date."),
		t("gl_vouchers", "All vouchers (journal entries) posted within a date range. Requires from_date and to_date."),
		t("gl_voucher_detail", "Full detail for a single voucher. Requires series and number."),
		t("gl_predefined_accounts", "Return the system-defined account mappings (e.g. VAT, retained earnings)."),
		t("gl_financial_years", "List all financial years configured in Fortnox."),
		t("project_list", "List all projects in Fortnox."),
		t("project_transactions", "All voucher rows posted to a project within an optional date range. Requires project_id."),
		t("project_profitability", "Calculate project profitability by summing debit (cost) vs credit (revenue) rows. Requires project_id."),
		t("costcenter_list", "List all cost centers defined in Fortnox."),
		t("costcenter_transactions", "All voucher rows posted to a cost center within an optional date range. Requires code."),
		t("costcenter_analysis", "Group all cost center transactions by account number, showing debit, credit and net. Requires code."),
		t("asset_list", "List all fixed assets in the asset register."),
		t("asset_detail", "Full detail for a single fixed asset. Requires asset_id."),
		t("cash_flow_forecast", "Forecast cash flow over a horizon: inflows from unpaid AR, outflows from unpaid AP, both filtered to invoices due within the horizon."),
		t("expense_analysis", "Group debit amounts for BAS expense accounts (4000-7999) by account for a date range. Requires from_date and to_date."),
		t("period_comparison", "Compare revenue and COGS side-by-side for two date ranges. Requires period1_from, period1_to, period2_from, period2_to."),
		t("yearly_comparison", "Compare account balances for two financial years side-by-side. Requires year1 and year2."),
		t("gross_margin_trend", "Monthly gross margin trend: revenue, COGS and margin % for each of the last N months."),
		t("top_customers", "Rank customers by unpaid AR invoice totals for a date range. Requires from_date and to_date."),
		t("top_suppliers", "Rank suppliers by unpaid AP invoice totals for a date range. Requires from_date and to_date."),
		t("sales_vs_purchases", "Compare revenue and COGS for a period, returning both totals and the net. Requires from_date and to_date."),
		t("company_info", "Return the company profile from the ERP."),
	}
}

// dispatchTool routes a tool name + raw JSON args to the matching MCP handler.
func (h *ChatHandler) dispatchTool(ctx context.Context, name string, rawInput any) (string, error) {
	args := map[string]any{}
	if rawInput != nil {
		if m, ok := rawInput.(map[string]any); ok {
			args = m
		}
	}

	req := mcp.CallToolRequest{}
	req.Params.Name = name
	req.Params.Arguments = args

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

	for _, c := range result.Content {
		if tc, ok := c.(mcp.TextContent); ok {
			return tc.Text, nil
		}
	}
	return `{}`, nil
}

func (h *ChatHandler) callLLM(ctx context.Context, req llmRequest) (*llmResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		h.llmCfg.BaseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+h.llmCfg.APIKey)

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
		return nil, fmt.Errorf("llm api error %d: %s", resp.StatusCode, respBody)
	}

	var lr llmResponse
	if err := json.Unmarshal(respBody, &lr); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &lr, nil
}

func (h *ChatHandler) MessageHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var body struct {
		Messages []llmMessage `json:"messages"`
		Message  string       `json:"message"`
		Escalate bool         `json:"escalate"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}

	model := h.llmCfg.DefaultModel
	if body.Escalate && h.llmCfg.EscalationModel != "" {
		model = h.llmCfg.EscalationModel
	}

	messages := []llmMessage{
		{
			Role:    "system",
			Content: "You are a financial analyst for a Swedish company using Fortnox ERP. Use your tools to answer questions about accounts payable (AP), accounts receivable (AR), general ledger (GL), projects, cost centers, and assets. Always use tools to fetch live data before answering. Respond in the same language as the user's question.",
		},
	}
	messages = append(messages, body.Messages...)
	messages = append(messages, llmMessage{Role: "user", Content: body.Message})

	req := llmRequest{
		Model:     model,
		Messages:  messages,
		Tools:     toolSchemas(),
		MaxTokens: 4096,
	}

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

	const maxTurns = 5
	for turn := range maxTurns {
		resp, err := h.callLLM(ctx, req)
		if err != nil {
			h.log.Error("llm call failed", "turn", turn, "err", err)
			sseWrite(fmt.Sprintf("[error: %v]", err))
			return
		}
		if len(resp.Choices) == 0 {
			sseWrite("[error: empty response from LLM]")
			return
		}

		choice := resp.Choices[0]

		if choice.FinishReason != "tool_calls" {
			if s, ok := choice.Message.Content.(string); ok && s != "" {
				sseWrite(s)
			}
			return
		}

		req.Messages = append(req.Messages, choice.Message)

		for _, tc := range choice.Message.ToolCalls {
			h.log.Info("executing tool", "name", tc.Function.Name)

			var args map[string]any
			if tc.Function.Arguments != "" {
				_ = json.Unmarshal([]byte(tc.Function.Arguments), &args)
			}

			result, toolErr := h.dispatchTool(ctx, tc.Function.Name, args)
			if toolErr != nil {
				result = fmt.Sprintf(`{"error": %q}`, toolErr.Error())
			}

			req.Messages = append(req.Messages, llmMessage{
				Role:       "tool",
				ToolCallID: tc.ID,
				Content:    result,
			})
		}
	}

	sseWrite("[max tool turns reached]")
}
