# v0.7.0 — LiteLLM Chat Integration Design

**Date:** 2026-05-04  
**Status:** Approved

## Problem

The cobalt-dingo HTMX chat currently calls the Anthropic Messages API directly using Claude-specific message formats (`tool_use`, `tool_result`, `stop_reason`). This ties the chat to a single cloud provider, sends financial data to Anthropic, and incurs token costs for every operational query. The goal is to route the chat through the local LiteLLM gateway with Gemma 4 31B as the default, keeping Fortnox data on-prem and enabling an optional escalation path to a stronger model when needed.

## Architecture

Single code path. Everything goes through the LiteLLM gateway at `https://llm-api.d-ma.be` using the OpenAI-compatible `/v1/chat/completions` endpoint. Default model: `iguana/gemma4-31b`. Optional escalation model: configurable via env var — if set, a toggle appears in the chat UI. No dual code paths, no Anthropic-specific types in the chat handler.

The `dispatchTool` function and the 38 MCP tool definitions are unchanged.

## Components

### 1. Config (`internal/config/config.go`)

New `LLM` struct replaces `Claude`:

```go
type LLM struct {
    BaseURL         string // https://llm-api.d-ma.be
    APIKey          string // LiteLLM master key (Bearer token)
    DefaultModel    string // e.g. "iguana/gemma4-31b"
    EscalationModel string // optional; empty = no escalation button shown
}
```

`LoadLLM()` reads from:
- `LLM_BASE_URL`
- `LLM_API_KEY`
- `LLM_DEFAULT_MODEL` (default: `iguana/gemma4-31b` if unset)
- `LLM_ESCALATION_MODEL` (optional)

Chat is enabled when `BaseURL`, `APIKey`, and `DefaultModel` are all non-empty.

`config.Claude` and `LoadClaude()` are removed. `ANTHROPIC_API_KEY` / `CLAUDE_MODEL` are removed from the chat path (they may remain in `.env` for Claude Code's own use, but cobalt-dingo no longer reads them for chat).

### 2. Chat handler (`internal/ui/chat.go`)

#### Types (OpenAI format)

Replace all Anthropic-specific types with OpenAI-compatible equivalents:

```go
type llmMessage struct {
    Role       string     `json:"role"`                 // system|user|assistant|tool
    Content    any        `json:"content"`              // string | []contentPart
    ToolCalls  []toolCall `json:"tool_calls,omitempty"`
    ToolCallID string     `json:"tool_call_id,omitempty"`
    Name       string     `json:"name,omitempty"`
}

type toolCall struct {
    ID       string       `json:"id"`
    Type     string       `json:"type"` // "function"
    Function toolCallFunc `json:"function"`
}

type toolCallFunc struct {
    Name      string `json:"name"`
    Arguments string `json:"arguments"` // JSON string
}

type llmRequest struct {
    Model    string       `json:"model"`
    Messages []llmMessage `json:"messages"`
    Tools    []llmTool    `json:"tools"`
    MaxTokens int         `json:"max_tokens"`
}

type llmTool struct {
    Type     string      `json:"type"` // "function"
    Function llmFunction `json:"function"`
}

type llmFunction struct {
    Name        string         `json:"name"`
    Description string         `json:"description"`
    Parameters  map[string]any `json:"parameters"`
}

type llmResponse struct {
    Choices []struct {
        Message      llmMessage `json:"message"`
        FinishReason string     `json:"finish_reason"` // "stop" | "tool_calls"
    } `json:"choices"`
}
```

#### Tool schemas

`toolSchemas()` returns `[]llmTool` with `type: "function"` and `parameters` (not `input_schema`). All 38 tool definitions carry over unchanged in their names and descriptions.

#### `callLLM(ctx, req llmRequest) (*llmResponse, error)`

- `POST {BaseURL}/v1/chat/completions`
- `Authorization: Bearer {APIKey}`
- `Content-Type: application/json`
- Returns parsed `llmResponse`

#### Agentic loop

```
for turn in 0..maxTurns:
    resp = callLLM(req)
    if finish_reason == "stop":
        stream resp.choices[0].message.content via SSE
        return
    if finish_reason == "tool_calls":
        append assistant message (with tool_calls) to req.Messages
        for each tool_call:
            result = dispatchTool(name, args)
            append role="tool" message with tool_call_id and result
        continue
```

Compared to current: `stop_reason == "tool_use"` → `finish_reason == "tool_calls"`. Tool results are `role: "tool"` messages, not `tool_result` content blocks.

#### Model selection per request

`MessageHandler` reads an `escalate` bool from the POST JSON body:
```json
{ "messages": [...], "message": "...", "escalate": false }
```
If `escalate: true` and `EscalationModel` is non-empty, use escalation model. Otherwise use `DefaultModel`.

### 3. UI (`internal/ui/chat.templ`)

- Banner gains model name: e.g. `SANDBOX · iguana/gemma4-31b`
- Escalation toggle button rendered only when `EscalationModel != ""`. Clicking it sets a JS flag that adds `"escalate": true` to the next POST body.
- Active model indicator updates when escalation is toggled.

### 4. Server wiring (`cmd/server/main.go`)

```go
llmCfg := config.LoadLLM()
if llmCfg.IsEnabled() && fortnoxEnabled {
    chatHandler := ui.NewChatHandler(mcpDeps, llmCfg, cfg.Mode, log)
    mux.HandleFunc("GET /chat", chatHandler.PageHandler)
    mux.HandleFunc("POST /chat", chatHandler.MessageHandler)
}
```

`NewChatHandler` takes `config.LLM` instead of `config.Claude`.

### 5. `.env.example`

Add LLM block, remove Claude block:

```
# ── LLM gateway (LiteLLM) ───────────────────────────────────────────────────
LLM_BASE_URL=https://llm-api.d-ma.be
LLM_API_KEY=
LLM_DEFAULT_MODEL=iguana/gemma4-31b
LLM_ESCALATION_MODEL=   # optional; leave empty to hide escalation button
```

## Data flow

```
Browser → POST /chat
  → ChatHandler.MessageHandler
    → select model (default or escalation)
    → build llmRequest (OpenAI format, system prompt + history + tools)
    → callLLM → LiteLLM (llm-api.d-ma.be)
      → LiteLLM → Ollama on iguana → Gemma 4 31B
    → if finish_reason == "tool_calls":
        → dispatchTool → MCP handler → Fortnox adapter → Fortnox API
        → append tool result, loop
    → SSE stream final text → Browser
```

Fortnox data never leaves iguana (MCP tools run locally; only the final text answer reaches the browser).

## Error handling

- `callLLM` non-200: return error string via SSE, log with slog
- Tool dispatch error: return `{"error": "..."}` as tool result, continue loop (same as today)
- Max turns exceeded: `[max tool turns reached]` via SSE (same as today)
- LiteLLM unreachable: surface as chat error, do not crash server

## Testing

- `TestCallLLM_BuildsCorrectRequest`: httptest server verifies auth header, model, tool format
- `TestAgenticLoop_ToolCallsAndStop`: mock LiteLLM returns tool_calls then stop; verify tool dispatch called and SSE output correct
- `TestLoadLLM_Defaults` and `TestLoadLLM_MissingRequired`: config unit tests
- Manual smoke test: `task dev` with `LLM_BASE_URL` set → chat page → ask "what's my AP backlog?"

## Out of scope (v0.7.0)

- Streaming from LiteLLM to browser (currently buffered; remains buffered)
- Adding Claude to LiteLLM config (user configures separately when needed)
- Invoice intake pipeline (deferred, separate feature)
- Chart.js rendering tests
