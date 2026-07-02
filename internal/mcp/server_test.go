package mcp_test

import (
	"context"
	"encoding/json"
	"testing"

	mcpserver "github.com/mathiasb/cobalt-dingo/internal/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewServerReportsInjectedVersion verifies that the version passed to
// NewServer is surfaced in the MCP initialize handshake's serverInfo, rather
// than a hardcoded constant.
func TestNewServerReportsInjectedVersion(t *testing.T) {
	const wantVersion = "9.9.9-test"

	srv := mcpserver.NewServer(mcpserver.Deps{}, wantVersion)

	initReq := []byte(`{
		"jsonrpc": "2.0",
		"id": 1,
		"method": "initialize",
		"params": {
			"protocolVersion": "2024-11-05",
			"capabilities": {},
			"clientInfo": {"name": "test-client", "version": "1.0.0"}
		}
	}`)

	resp := srv.HandleMessage(context.Background(), initReq)
	require.NotNil(t, resp)

	raw, err := json.Marshal(resp)
	require.NoError(t, err)

	var parsed struct {
		Result struct {
			ServerInfo struct {
				Name    string `json:"name"`
				Version string `json:"version"`
			} `json:"serverInfo"`
		} `json:"result"`
	}
	require.NoError(t, json.Unmarshal(raw, &parsed))

	assert.Equal(t, "cobalt-dingo", parsed.Result.ServerInfo.Name)
	assert.Equal(t, wantVersion, parsed.Result.ServerInfo.Version)
}
