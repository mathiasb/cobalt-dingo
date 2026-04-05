// Package mcp implements the Model Context Protocol server that exposes
// Fortnox tools to Claude. Communication is over stdio.
//
// Security levels enforced here:
//   - Grön (automatic): all read operations
//   - Gul (requires "ja"): write operations – enforced by the bookkeeper agent prompt
//   - Röd (always blocked): delete, VAT submission, third-party sharing
package mcp

import (
	"context"
	"fmt"

	"github.com/mathiasb/coo-agent/internal/api"
	"github.com/mathiasb/coo-agent/internal/audit"
	"github.com/mathiasb/coo-agent/internal/validator"
)

// Server is the MCP server that exposes Fortnox tools to Claude.
type Server struct {
	api       *api.Client
	validator *validator.Validator
	auditLog  *audit.Logger
}

// NewServer creates a new MCP server.
func NewServer(client *api.Client, val *validator.Validator, auditLog *audit.Logger) *Server {
	return &Server{
		api:       client,
		validator: val,
		auditLog:  auditLog,
	}
}

// Start registers all tools and begins serving over stdio.
// Blocks until ctx is cancelled.
// TODO: integrate mark3labs/mcp-go to implement the MCP protocol.
func (s *Server) Start(_ context.Context) error {
	return fmt.Errorf("mcp: server not yet implemented – wire up mark3labs/mcp-go")
}
