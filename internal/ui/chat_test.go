package ui

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
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
		log:  slog.Default(),
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

func TestMessageHandler_DirectResponse(t *testing.T) {
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
