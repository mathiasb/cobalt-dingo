# v0.7.0 LiteLLM Chat Integration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the Anthropic Messages API chat handler with an OpenAI-compatible handler that routes through LiteLLM, defaulting to `iguana/gemma4-31b` with an optional escalation model.

**Architecture:** Single code path — all LLM calls go to `{LLM_BASE_URL}/v1/chat/completions` with a Bearer token. The agentic loop is rewritten from Anthropic (`tool_use`/`tool_result`/`stop_reason`) to OpenAI (`tool_calls`/`role:tool`/`finish_reason`) format. The `dispatchTool` function and 38 MCP tool definitions are unchanged.

**Tech Stack:** Go stdlib `net/http`, `encoding/json`, `net/http/httptest` for tests, `github.com/mark3labs/mcp-go`, `github.com/a-h/templ`

---

## File Map

| File | Action |
|------|--------|
| `internal/config/config.go` | Add `LLM` struct + `LoadLLM()`, remove `Claude` + `LoadClaude()` |
| `internal/config/config_test.go` | Add `TestLoadLLM_*` tests |
| `internal/ui/chat.go` | Replace Anthropic types with OpenAI types, rewrite `callLLM` + `MessageHandler`, update constructor |
| `internal/ui/chat_test.go` | New file: `TestCallLLM_*`, `TestMessageHandler_*` |
| `internal/ui/chat.templ` | Update `ChatPage` signature, add model to banner, add escalation toggle |
| `internal/ui/chat_templ.go` | Regenerated — run `templ generate ./...`, never hand-edit |
| `cmd/server/main.go` | Swap `LoadClaude` → `LoadLLM`, update `NewChatHandler` call |
| `.env.example` | Replace Claude block with LLM gateway block |

---

## Task 1: Config — LLM struct and LoadLLM()

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/config/config_test.go`

- [ ] **Step 1: Write the failing tests**

Add to `internal/config/config_test.go` (below existing tests):

```go
func TestLoadLLM_Defaults(t *testing.T) {
	t.Setenv("LLM_BASE_URL", "https://llm-api.example.com")
	t.Setenv("LLM_API_KEY", "test-key")
	// LLM_DEFAULT_MODEL unset → should default to "iguana/gemma4-31b"
	// LLM_ESCALATION_MODEL unset → should be ""

	cfg := LoadLLM()

	assert.Equal(t, "https://llm-api.example.com", cfg.BaseURL)
	assert.Equal(t, "test-key", cfg.APIKey)
	assert.Equal(t, "iguana/gemma4-31b", cfg.DefaultModel)
	assert.Equal(t, "", cfg.EscalationModel)
}

func TestLoadLLM_Explicit(t *testing.T) {
	t.Setenv("LLM_BASE_URL", "https://llm-api.example.com")
	t.Setenv("LLM_API_KEY", "sk-abc")
	t.Setenv("LLM_DEFAULT_MODEL", "koala/phi4-14b")
	t.Setenv("LLM_ESCALATION_MODEL", "berget/llama-3.3-70b")

	cfg := LoadLLM()

	assert.Equal(t, "koala/phi4-14b", cfg.DefaultModel)
	assert.Equal(t, "berget/llama-3.3-70b", cfg.EscalationModel)
}

func TestLoadLLM_Empty(t *testing.T) {
	// No env vars set — all fields empty, IsEnabled false.
	cfg := LoadLLM()

	assert.Empty(t, cfg.BaseURL)
	assert.Empty(t, cfg.APIKey)
	assert.False(t, cfg.IsEnabled())
}

func TestLLM_IsEnabled(t *testing.T) {
	tests := []struct {
		name    string
		cfg     LLM
		enabled bool
	}{
		{"all set", LLM{BaseURL: "http://x", APIKey: "k", DefaultModel: "m"}, true},
		{"missing BaseURL", LLM{APIKey: "k", DefaultModel: "m"}, false},
		{"missing APIKey", LLM{BaseURL: "http://x", DefaultModel: "m"}, false},
		{"missing DefaultModel", LLM{BaseURL: "http://x", APIKey: "k"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.enabled, tt.cfg.IsEnabled())
		})
	}
}
```

- [ ] **Step 2: Run tests to confirm they fail**

```bash
go test ./internal/config/... -run TestLoadLLM -v
```

Expected: `FAIL — undefined: LoadLLM`

- [ ] **Step 3: Add LLM struct and LoadLLM() to config.go**

In `internal/config/config.go`, add after the `Debtor` block and remove the `Claude` struct + `LoadClaude()`:

```go
// LLM holds configuration for the LiteLLM gateway used by the chat interface.
type LLM struct {
	BaseURL         string // e.g. "https://llm-api.d-ma.be"
	APIKey          string // LiteLLM master key (Bearer token)
	DefaultModel    string // e.g. "iguana/gemma4-31b"
	EscalationModel string // optional; empty = no escalation button shown
}

// IsEnabled reports whether the LLM gateway is fully configured.
// Chat is enabled only when all three required fields are non-empty.
func (l LLM) IsEnabled() bool {
	return l.BaseURL != "" && l.APIKey != "" && l.DefaultModel != ""
}

// LoadLLM reads LLM gateway config from environment variables.
// LLM_DEFAULT_MODEL defaults to "iguana/gemma4-31b" if unset.
func LoadLLM() LLM {
	model := os.Getenv("LLM_DEFAULT_MODEL")
	if model == "" {
		model = "iguana/gemma4-31b"
	}
	return LLM{
		BaseURL:         os.Getenv("LLM_BASE_URL"),
		APIKey:          os.Getenv("LLM_API_KEY"),
		DefaultModel:    model,
		EscalationModel: os.Getenv("LLM_ESCALATION_MODEL"),
	}
}
```

Delete these lines from `config.go`:

```go
// Claude holds Claude API configuration for the chat interface.
type Claude struct {
	APIKey string
	Model  string
}

// LoadClaude reads Claude config from environment variables.
func LoadClaude() Claude {
	model := os.Getenv("CLAUDE_MODEL")
	if model == "" {
		model = "claude-sonnet-4-6"
	}
	return Claude{
		APIKey: os.Getenv("ANTHROPIC_API_KEY"),
		Model:  model,
	}
}
```

- [ ] **Step 4: Run tests to confirm they pass**

```bash
go test ./internal/config/... -v
```

Expected: all tests PASS (existing Mode/Load tests still green).

- [ ] **Step 5: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat(config): add LLM struct + LoadLLM(), remove Claude"
```

---

## Task 2: Chat handler — OpenAI types and callLLM

**Files:**
- Modify: `internal/ui/chat.go`
- Create: `internal/ui/chat_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/ui/chat_test.go`:

```go
package ui

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mathiasb/cobalt-dingo/internal/config"
	mcpserver "github.com/mathiasb/cobalt-dingo/internal/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestChatHandler(baseURL string) *ChatHandler {
	return &ChatHandler{
		llmCfg: config.LLM{
			BaseURL:      baseURL,
			APIKey:       "test-key",
			DefaultModel: "iguana/gemma4-31b",
		},
		deps: mcpserver.Deps{},
	}
}

func TestCallLLM_BuildsCorrectRequest(t *testing.T) {
	var gotAuth, gotModel string
	var gotTools []llmTool

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		var req llmRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		gotModel = req.Model
		gotTools = req.Tools

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(llmResponse{
			Choices: []llmChoice{{
				Message:      llmMessage{Role: "assistant", Content: "pong"},
				FinishReason: "stop",
			}},
		})
	}))
	defer srv.Close()

	h := newTestChatHandler(srv.URL)
	req := llmRequest{
		Model:     "iguana/gemma4-31b",
		Messages:  []llmMessage{{Role: "user", Content: "ping"}},
		Tools:     toolSchemas(),
		MaxTokens: 4096,
	}
	resp, err := h.callLLM(context.Background(), req)

	require.NoError(t, err)
	assert.Equal(t, "Bearer test-key", gotAuth)
	assert.Equal(t, "iguana/gemma4-31b", gotModel)
	assert.NotEmpty(t, gotTools)
	assert.Equal(t, "function", gotTools[0].Type)
	assert.NotEmpty(t, gotTools[0].Function.Name)
	assert.NotEmpty(t, gotTools[0].Function.Parameters)
	assert.Equal(t, "stop", resp.Choices[0].FinishReason)
}

func TestCallLLM_NonOKReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "rate limited", http.StatusTooManyRequests)
	}))
	defer srv.Close()

	h := newTestChatHandler(srv.URL)
	_, err := h.callLLM(context.Background(), llmRequest{
		Model:     "iguana/gemma4-31b",
		Messages:  []llmMessage{{Role: "user", Content: "hi"}},
		MaxTokens: 100,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "429")
}
```

- [ ] **Step 2: Run test to confirm it fails**

```bash
go test ./internal/ui/... -run TestCallLLM -v
```

Expected: `FAIL — undefined: llmRequest, llmTool, llmResponse, llmChoice, llmMessage, callLLM`

- [ ] **Step 3: Replace Anthropic types with OpenAI types in chat.go**

Replace the type block (lines 37–77 in the current file — `chatMessage`, `contentBlock`, `toolDef`, `claudeRequest`, `claudeResponse`) with:

```go
// llmMessage is a chat message in OpenAI format.
type llmMessage struct {
	Role       string     `json:"role"`                   // system|user|assistant|tool
	Content    any        `json:"content,omitempty"`      // string or nil when tool_calls present
	ToolCalls  []toolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"` // set on role=tool messages
	Name       string     `json:"name,omitempty"`
}

type toolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"` // always "function"
	Function toolCallFunc `json:"function"`
}

type toolCallFunc struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // JSON-encoded string
}

type llmRequest struct {
	Model     string       `json:"model"`
	Messages  []llmMessage `json:"messages"`
	Tools     []llmTool    `json:"tools,omitempty"`
	MaxTokens int          `json:"max_tokens"`
}

type llmTool struct {
	Type     string      `json:"type"` // always "function"
	Function llmFunction `json:"function"`
}

type llmFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

type llmChoice struct {
	Message      llmMessage `json:"message"`
	FinishReason string     `json:"finish_reason"` // "stop" | "tool_calls"
}

type llmResponse struct {
	Choices []llmChoice `json:"choices"`
}
```

- [ ] **Step 4: Update ChatHandler struct and constructor**

Replace the `ChatHandler` struct and `NewChatHandler`:

```go
// ChatHandler handles the /chat page and message API.
type ChatHandler struct {
	deps   mcpserver.Deps
	llmCfg config.LLM
	mode   config.Mode
	log    *slog.Logger
}

// NewChatHandler constructs a ChatHandler wired to the given MCP deps and LLM config.
func NewChatHandler(deps mcpserver.Deps, llmCfg config.LLM, mode config.Mode, log *slog.Logger) *ChatHandler {
	return &ChatHandler{deps: deps, llmCfg: llmCfg, mode: mode, log: log}
}
```

- [ ] **Step 5: Replace callClaude with callLLM**

Replace the `callClaude` method with:

```go
// callLLM posts a request to the LiteLLM gateway (OpenAI-compatible) and returns the response.
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
```

- [ ] **Step 6: Update toolSchemas() to OpenAI format**

Replace the existing `toolSchemas()` function with:

```go
// toolSchemas returns tool definitions in OpenAI function-calling format for all 38 MCP tools.
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
```

- [ ] **Step 7: Run tests**

```bash
go test ./internal/ui/... -run TestCallLLM -v
```

Expected: both `TestCallLLM_BuildsCorrectRequest` and `TestCallLLM_NonOKReturnsError` PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/ui/chat.go internal/ui/chat_test.go
git commit -m "feat(chat): replace Anthropic types with OpenAI format, add callLLM"
```

---

## Task 3: Agentic loop rewrite

**Files:**
- Modify: `internal/ui/chat.go` (MessageHandler only)
- Modify: `internal/ui/chat_test.go` (add loop tests)

- [ ] **Step 1: Write failing loop tests**

Add to `internal/ui/chat_test.go`:

```go
func TestMessageHandler_DirectResponse(t *testing.T) {
	// LiteLLM returns stop immediately — no tool calls.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(llmResponse{
			Choices: []llmChoice{{
				Message:      llmMessage{Role: "assistant", Content: "AP looks clean."},
				FinishReason: "stop",
			}},
		})
	}))
	defer srv.Close()

	h := newTestChatHandler(srv.URL)
	body := `{"message":"show AP","messages":[]}`
	req := httptest.NewRequest(http.MethodPost, "/chat", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.MessageHandler(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "data: AP looks clean.")
}

func TestMessageHandler_ToolCallsThenStop(t *testing.T) {
	// First call: LiteLLM requests an (unknown) tool.
	// Second call: LiteLLM returns a direct response.
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		if callCount == 1 {
			_ = json.NewEncoder(w).Encode(llmResponse{
				Choices: []llmChoice{{
					Message: llmMessage{
						Role: "assistant",
						ToolCalls: []toolCall{{
							ID:   "call_1",
							Type: "function",
							Function: toolCallFunc{
								Name:      "nonexistent_tool",
								Arguments: `{}`,
							},
						}},
					},
					FinishReason: "tool_calls",
				}},
			})
		} else {
			_ = json.NewEncoder(w).Encode(llmResponse{
				Choices: []llmChoice{{
					Message:      llmMessage{Role: "assistant", Content: "Done."},
					FinishReason: "stop",
				}},
			})
		}
	}))
	defer srv.Close()

	h := newTestChatHandler(srv.URL)
	body := `{"message":"run a tool","messages":[]}`
	req := httptest.NewRequest(http.MethodPost, "/chat", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.MessageHandler(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, 2, callCount)
	assert.Contains(t, w.Body.String(), "data: Done.")
}

func TestMessageHandler_EscalateModel(t *testing.T) {
	var gotModel string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req llmRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		gotModel = req.Model
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(llmResponse{
			Choices: []llmChoice{{
				Message:      llmMessage{Role: "assistant", Content: "ok"},
				FinishReason: "stop",
			}},
		})
	}))
	defer srv.Close()

	h := newTestChatHandler(srv.URL)
	h.llmCfg.EscalationModel = "berget/llama-3.3-70b"

	body := `{"message":"hard question","messages":[],"escalate":true}`
	req := httptest.NewRequest(http.MethodPost, "/chat", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.MessageHandler(w, req)

	assert.Equal(t, "berget/llama-3.3-70b", gotModel)
}
```

Add `"strings"` to the imports in `chat_test.go`.

- [ ] **Step 2: Run to confirm they fail**

```bash
go test ./internal/ui/... -run TestMessageHandler -v
```

Expected: FAIL — old `MessageHandler` uses `claudeRequest`/`callClaude` which no longer exist.

- [ ] **Step 3: Rewrite MessageHandler**

Replace the entire `MessageHandler` method in `internal/ui/chat.go`:

```go
// MessageHandler handles POST /chat — processes the user message and streams back via SSE.
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

	// Build message history: system prompt + existing history + new user message.
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

	// Set SSE headers.
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

	// Agentic loop: call LLM, execute tools, repeat until finish_reason != "tool_calls".
	const maxTurns = 5
	for turn := 0; turn < maxTurns; turn++ {
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
			// Final text response — stream it.
			if s, ok := choice.Message.Content.(string); ok && s != "" {
				sseWrite(s)
			}
			return
		}

		// Append assistant turn (with tool_calls) to history.
		req.Messages = append(req.Messages, choice.Message)

		// Execute each tool and append results.
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
```

- [ ] **Step 4: Run all UI tests**

```bash
go test ./internal/ui/... -v
```

Expected: all 5 tests PASS.

- [ ] **Step 5: Build to catch any remaining type errors**

```bash
go build ./...
```

Fix any compile errors before proceeding.

- [ ] **Step 6: Commit**

```bash
git add internal/ui/chat.go internal/ui/chat_test.go
git commit -m "feat(chat): rewrite agentic loop to OpenAI format via LiteLLM"
```

---

## Task 4: UI — banner update and escalation toggle

**Files:**
- Modify: `internal/ui/chat.templ`
- Regenerate: `internal/ui/chat_templ.go`

- [ ] **Step 1: Update ChatPage signature and banner in chat.templ**

Replace the entire `internal/ui/chat.templ` content:

```go
package ui

import (
	"github.com/mathiasb/cobalt-dingo/internal/config"
)

// modeBannerStyle returns inline CSS for the mode stripe in the chat banner.
func modeBannerStyle(m config.Mode) string {
	switch m {
	case config.ModeSandbox:
		return "background:#d1fae5;color:#065f46;border:1px solid #6ee7b7;"
	case config.ModeRealReadonly:
		return "background:#fef3c7;color:#78350f;border:1px solid #fcd34d;"
	}
	return "background:#fee2e2;color:#7f1d1d;border:1px solid #fca5a5;"
}

templ ChatPage(mode config.Mode, llm config.LLM) {
	@Layout("Chat") {
		<div style={ "padding:0.5rem 0.75rem;border-radius:0.375rem;font-size:0.8125rem;font-weight:500;margin-bottom:1rem;display:flex;justify-content:space-between;align-items:center;" + modeBannerStyle(mode) }>
			<span>Mode: { mode.Label() } &nbsp;·&nbsp; Model: <span id="active-model">{ llm.DefaultModel }</span></span>
			<div style="display:flex;gap:0.5rem;align-items:center;">
				if mode.AllowsWrites() {
					<span style="font-weight:400;opacity:0.8;">writes enabled</span>
				} else {
					<span style="font-weight:400;opacity:0.8;">read-only</span>
				}
				if llm.EscalationModel != "" {
					<button
						id="escalate-btn"
						type="button"
						data-default={ llm.DefaultModel }
						data-escalation={ llm.EscalationModel }
						style="font-size:0.75rem;padding:0.2rem 0.6rem;border-radius:0.375rem;border:1px solid currentColor;background:transparent;cursor:pointer;opacity:0.8;"
					>Escalate</button>
				}
			</div>
		</div>
		<style>
			.chat-container {
				display: flex;
				flex-direction: column;
				height: calc(100vh - 12rem);
				min-height: 400px;
			}
			.chat-thread {
				flex: 1;
				overflow-y: auto;
				padding: 1rem 0;
				display: flex;
				flex-direction: column;
				gap: 1rem;
			}
			.chat-bubble {
				max-width: 75%;
				padding: 0.75rem 1rem;
				border-radius: 0.75rem;
				line-height: 1.5;
				white-space: pre-wrap;
				word-break: break-word;
			}
			.chat-bubble-user {
				align-self: flex-end;
				background: var(--gds-sys-color-positive-100, #d1fae5);
				color: var(--gds-sys-color-base-900);
				border-bottom-right-radius: 0.125rem;
			}
			.chat-bubble-assistant {
				align-self: flex-start;
				background: var(--gds-sys-color-background-secondary, #f3f4f6);
				color: var(--gds-sys-color-text-primary);
				border-bottom-left-radius: 0.125rem;
			}
			.chat-bubble-system {
				align-self: center;
				font-size: 0.75rem;
				color: var(--gds-sys-color-text-secondary);
				background: transparent;
				padding: 0.25rem 0.5rem;
			}
			.chat-input-row {
				display: flex;
				gap: 0.5rem;
				padding-top: 0.75rem;
				border-top: 1px solid var(--gds-sys-color-base-300);
			}
			.chat-input {
				flex: 1;
				padding: 0.625rem 0.875rem;
				border: 1px solid var(--gds-sys-color-base-300);
				border-radius: 0.5rem;
				font-size: 0.9375rem;
				background: var(--gds-sys-color-background-primary);
				color: var(--gds-sys-color-text-primary);
				resize: none;
				min-height: 2.5rem;
				max-height: 8rem;
				font-family: inherit;
			}
			.chat-input:focus {
				outline: none;
				border-color: var(--gds-sys-color-primary-500, #059669);
			}
			.chat-send-btn {
				padding: 0.625rem 1.25rem;
				background: var(--gds-sys-color-primary-500, #059669);
				color: #fff;
				border: none;
				border-radius: 0.5rem;
				font-size: 0.9375rem;
				cursor: pointer;
				white-space: nowrap;
				align-self: flex-end;
			}
			.chat-send-btn:hover { opacity: 0.9; }
			.chat-send-btn:disabled { opacity: 0.5; cursor: not-allowed; }
			.chat-thinking {
				align-self: flex-start;
				font-size: 0.875rem;
				color: var(--gds-sys-color-text-secondary);
				font-style: italic;
				animation: pulse 1.2s ease-in-out infinite;
			}
			@keyframes pulse {
				0%, 100% { opacity: 1; }
				50% { opacity: 0.4; }
			}
		</style>

		<div class="chat-container">
			<div class="chat-thread" id="chat-thread">
				<div class="chat-bubble chat-bubble-system">
					Ask questions about AP, AR, GL, projects, cost centres, and assets.
				</div>
			</div>
			<div class="chat-input-row">
				<textarea
					id="chat-input"
					class="chat-input"
					placeholder="e.g. What are my overdue supplier invoices?"
					rows="1"
				></textarea>
				<button id="chat-send" class="chat-send-btn" type="button">Send</button>
			</div>
		</div>

		<script>
		(function() {
			const thread    = document.getElementById('chat-thread');
			const input     = document.getElementById('chat-input');
			const btn       = document.getElementById('chat-send');
			const escBtn    = document.getElementById('escalate-btn');
			const modelSpan = document.getElementById('active-model');

			let history  = [];
			let escalate = false;

			if (escBtn) {
				escBtn.addEventListener('click', function() {
					escalate = !escalate;
					const label = escalate ? 'Default' : 'Escalate';
					const model = escalate ? escBtn.dataset.escalation : escBtn.dataset.default;
					escBtn.textContent = label;
					escBtn.style.fontWeight = escalate ? '700' : 'normal';
					if (modelSpan) modelSpan.textContent = model;
				});
			}

			function scrollBottom() { thread.scrollTop = thread.scrollHeight; }

			function addBubble(role, text) {
				const div = document.createElement('div');
				div.className = 'chat-bubble chat-bubble-' + role;
				div.textContent = text;
				thread.appendChild(div);
				scrollBottom();
				return div;
			}

			function addThinking() {
				const div = document.createElement('div');
				div.className = 'chat-thinking';
				div.textContent = 'Thinking…';
				thread.appendChild(div);
				scrollBottom();
				return div;
			}

			function setDisabled(val) {
				btn.disabled   = val;
				input.disabled = val;
			}

			async function send() {
				const text = input.value.trim();
				if (!text) return;

				input.value = '';
				input.style.height = 'auto';
				addBubble('user', text);
				setDisabled(true);
				const thinking = addThinking();

				try {
					const resp = await fetch('/chat', {
						method: 'POST',
						headers: {'Content-Type': 'application/json'},
						body: JSON.stringify({messages: history, message: text, escalate: escalate}),
					});

					if (!resp.ok) {
						thinking.remove();
						addBubble('system', 'Error: ' + resp.status + ' ' + resp.statusText);
						return;
					}

					const reader = resp.body.getReader();
					const dec    = new TextDecoder();
					let buf      = '';
					let content  = '';
					let bubble   = null;

					thinking.remove();

					while (true) {
						const {value, done} = await reader.read();
						if (done) break;

						buf += dec.decode(value, {stream: true});
						const lines = buf.split('\n');
						buf = lines.pop();

						for (const line of lines) {
							if (!line.startsWith('data: ')) continue;
							const chunk = line.slice(6);
							content += chunk;
							if (!bubble) {
								bubble = addBubble('assistant', content);
							} else {
								bubble.textContent = content;
								scrollBottom();
							}
						}
					}

					history.push({role: 'user',      content: text});
					history.push({role: 'assistant', content: content});

				} catch (err) {
					thinking.remove();
					addBubble('system', 'Error: ' + err.message);
				} finally {
					setDisabled(false);
					input.focus();
				}
			}

			btn.addEventListener('click', send);

			input.addEventListener('keydown', function(e) {
				if (e.key === 'Enter' && !e.shiftKey) {
					e.preventDefault();
					send();
				}
			});

			input.addEventListener('input', function() {
				this.style.height = 'auto';
				this.style.height = Math.min(this.scrollHeight, 128) + 'px';
			});
		})();
		</script>
	}
}
```

- [ ] **Step 2: Regenerate templ**

```bash
templ generate ./...
```

Expected: `internal/ui/chat_templ.go` updated, no errors.

- [ ] **Step 3: Build to check**

```bash
go build ./...
```

If `cmd/server/main.go` fails because `ChatPage` now requires two args — ignore for now, fix in Task 5.

- [ ] **Step 4: Commit templ changes**

```bash
git add internal/ui/chat.templ internal/ui/chat_templ.go
git commit -m "feat(ui): add model name + escalation toggle to chat banner"
```

---

## Task 5: Server wiring and .env.example

**Files:**
- Modify: `cmd/server/main.go`
- Modify: `.env.example`

- [ ] **Step 1: Update cmd/server/main.go**

Find the chat handler block (currently `claudeCfg := config.LoadClaude()` around line 115) and replace:

```go
// Before:
claudeCfg := config.LoadClaude()
if claudeCfg.APIKey != "" && fortnoxEnabled {
    // ...
    chatHandler := ui.NewChatHandler(mcpDeps, claudeCfg, cfg.Mode, log)
    // ...
    log.Info("chat handler registered")
} else {
    log.Info("chat handler disabled — set ANTHROPIC_API_KEY to enable")
}
```

With:

```go
llmCfg := config.LoadLLM()
if llmCfg.IsEnabled() && fortnoxEnabled {
    var tokenStore domain.TokenStore
    if appCfg.DatabaseURL != "" {
        store, _ := postgres.NewStore(appCfg.DatabaseURL)
        tokenStore = postgres.NewTokenStore(store)
    } else {
        tokenStore = file.NewTokenStore(cfg.Mode.TokenFile())
    }
    baseURL := cfg.BaseURL()
    readOnly := !cfg.Mode.AllowsWrites()
    gl := adapterfortnox.NewGeneralLedgerAdapter(baseURL, tokenStore, readOnly)
    mcpDeps := mcpserver.Deps{
        TenantID:    defaultTenantID,
        SupplierLdg: adapterfortnox.NewSupplierLedgerAdapter(baseURL, tokenStore, readOnly),
        CustomerLdg: adapterfortnox.NewCustomerLedgerAdapter(baseURL, tokenStore, readOnly),
        GeneralLdg:  gl,
        ProjectLdg:  adapterfortnox.NewProjectLedgerAdapter(baseURL, tokenStore, gl, readOnly),
        CostCtrLdg:  adapterfortnox.NewCostCenterLedgerAdapter(baseURL, tokenStore, gl, readOnly),
        AssetReg:    adapterfortnox.NewAssetRegisterAdapter(baseURL, tokenStore, readOnly),
        CompanyInf:  adapterfortnox.NewCompanyInfoAdapter(baseURL, tokenStore, readOnly),
    }
    chatHandler := ui.NewChatHandler(mcpDeps, llmCfg, cfg.Mode, log)
    mux.HandleFunc("GET /chat", chatHandler.PageHandler)
    mux.HandleFunc("POST /chat", chatHandler.MessageHandler)
    log.Info("chat handler registered", "model", llmCfg.DefaultModel)
} else {
    log.Info("chat handler disabled — set LLM_BASE_URL, LLM_API_KEY to enable")
}
```

Also update the `PageHandler` call in `chat.go` — find `render(w, r, ChatPage(h.mode))` and change to:

```go
func (h *ChatHandler) PageHandler(w http.ResponseWriter, r *http.Request) {
    render(w, r, ChatPage(h.mode, h.llmCfg))
}
```

- [ ] **Step 2: Update .env.example**

Find the Claude block:

```
# ── Claude API (chat UI) ────────────────────────────────────────────────────

ANTHROPIC_API_KEY=
CLAUDE_MODEL=claude-sonnet-4-6
```

Replace with:

```
# ── LLM gateway (LiteLLM) ────────────────────────────────────────────────────
# Point at your LiteLLM instance. LLM_DEFAULT_MODEL defaults to iguana/gemma4-31b
# if unset. Set LLM_ESCALATION_MODEL to show an escalation toggle in the chat UI.

LLM_BASE_URL=https://llm-api.d-ma.be
LLM_API_KEY=
LLM_DEFAULT_MODEL=iguana/gemma4-31b
LLM_ESCALATION_MODEL=
```

- [ ] **Step 3: Build clean**

```bash
go build ./...
```

Expected: clean build, no errors.

- [ ] **Step 4: Run full test suite**

```bash
task check
```

Expected: lint, tests, vet all PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/server/main.go internal/ui/chat.go .env.example
git commit -m "feat(server): wire LiteLLM chat handler, drop Anthropic dependency"
```

---

## Task 6: Tag and verify

- [ ] **Step 1: Update .env with LLM credentials**

In your local `.env`, set:
```
LLM_BASE_URL=https://llm-api.d-ma.be
LLM_API_KEY=<your LiteLLM master key>
LLM_DEFAULT_MODEL=iguana/gemma4-31b
```

- [ ] **Step 2: Smoke test**

```bash
task dev   # starts server with FORTNOX_MODE=sandbox
```

Open `http://localhost:8080/chat`. The banner should show:
```
Mode: SANDBOX · Model: iguana/gemma4-31b    [read-only]
```

Ask: "What are my overdue supplier invoices?" — verify Gemma responds with real sandbox data.

- [ ] **Step 3: Push to Gitea and verify CI**

```bash
git push gitea main
```

Watch CI at `https://gitea.d-ma.be/mathias/cobalt-dingo/actions`. All 5 jobs should be green. (Chat handler won't activate in CI since `LLM_BASE_URL` is not set there — handler gracefully disabled.)

- [ ] **Step 4: Tag v0.7.0**

```bash
task tag version=v0.7.0
```

---

## Self-Review Checklist

**Spec coverage:**
- [x] `config.LLM` struct + `LoadLLM()` + `IsEnabled()` — Task 1
- [x] `callLLM` with Bearer auth + correct endpoint — Task 2
- [x] OpenAI tool schema format (`parameters`, not `input_schema`) — Task 2
- [x] Agentic loop: `finish_reason == "tool_calls"` detection — Task 3
- [x] Tool result as `role: "tool"` message with `tool_call_id` — Task 3
- [x] `escalate` bool in POST body → model selection — Task 3
- [x] Banner shows model name — Task 4
- [x] Escalation toggle button (conditional on `EscalationModel != ""`) — Task 4
- [x] JS updates model indicator on toggle — Task 4
- [x] Server gates on `llmCfg.IsEnabled()` — Task 5
- [x] `.env.example` updated — Task 5
- [x] Error handling: non-200, empty choices, unreachable — Task 3 (`MessageHandler`) + Task 2 (`callLLM`)
- [x] `Claude` struct and `LoadClaude()` removed — Task 1

**Placeholder scan:** No TBD, no TODO, all code blocks complete.

**Type consistency:**
- `llmMessage`, `toolCall`, `toolCallFunc`, `llmRequest`, `llmTool`, `llmFunction`, `llmChoice`, `llmResponse` — defined in Task 2, used consistently in Tasks 3, 4, 5.
- `config.LLM` — defined Task 1, consumed Task 2 (`newTestChatHandler`), Task 5 (`LoadLLM`, `NewChatHandler`).
- `ChatHandler.llmCfg config.LLM` — set Task 2, read in Task 3 (`DefaultModel`, `EscalationModel`), Task 4 (`PageHandler` call).
