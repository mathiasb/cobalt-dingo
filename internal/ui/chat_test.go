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
