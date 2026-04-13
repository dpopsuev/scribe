// Package mcpserver provides a Battery-integrated MCP server framework.
// It eliminates boilerplate by wrapping sdkmcp.Server with auto-Observable,
// result helpers, and a fluent builder API.
package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/dpopsuev/battery/server"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// ErrHandlerPanicked is returned when a tool handler panics.
var ErrHandlerPanicked = errors.New("battery: handler panicked")

// Server wraps sdkmcp.Server with Battery conventions.
type Server struct {
	sdk          *sdkmcp.Server
	name         string
	version      string
	instructions string
}

// NewServer creates a new Battery MCP server with the given name and version.
func NewServer(name, version string) *Server {
	return &Server{
		name:    name,
		version: version,
	}
}

// WithInstructions sets the MCP server instructions shown to clients.
func (s *Server) WithInstructions(instructions string) *Server {
	s.instructions = instructions
	return s
}

// build initializes the underlying sdkmcp.Server lazily on first use.
func (s *Server) build() {
	if s.sdk != nil {
		return
	}
	var opts *sdkmcp.ServerOptions
	if s.instructions != "" {
		opts = &sdkmcp.ServerOptions{Instructions: s.instructions}
	}
	s.sdk = sdkmcp.NewServer(
		&sdkmcp.Implementation{Name: s.name, Version: s.version},
		opts,
	)
}

// Tool registers a tool using server.ToolMeta for metadata and server.Handler
// for the handler function. The handler is auto-wrapped with Observable for
// timing/logging. InputSchema defaults to {"type":"object"}.
func (s *Server) Tool(meta server.ToolMeta, handler server.Handler) *Server {
	s.build()

	observed := server.Observable(meta.Name, handler)

	s.sdk.AddTool(
		&sdkmcp.Tool{
			Name:        meta.Name,
			Description: meta.Description,
			InputSchema: map[string]any{"type": "object"},
		},
		adaptHandler(observed),
	)
	return s
}

// ToolWithSchema registers a tool with an explicit JSON input schema.
func (s *Server) ToolWithSchema(meta server.ToolMeta, schema json.RawMessage, handler server.Handler) *Server {
	s.build()

	observed := server.Observable(meta.Name, handler)

	var schemaObj any
	if err := json.Unmarshal(schema, &schemaObj); err != nil {
		schemaObj = map[string]any{"type": "object"}
	}

	s.sdk.AddTool(
		&sdkmcp.Tool{
			Name:        meta.Name,
			Description: meta.Description,
			InputSchema: schemaObj,
		},
		adaptHandler(observed),
	)
	return s
}

// Serve starts the MCP server on the given transport. Blocks until ctx is canceled
// or the connection is closed.
func (s *Server) Serve(ctx context.Context, transport sdkmcp.Transport) error {
	s.build()
	if err := s.sdk.Run(ctx, transport); err != nil {
		return fmt.Errorf("battery: server run: %w", err)
	}
	return nil
}

// SDK returns the underlying sdkmcp.Server for advanced use cases.
func (s *Server) SDK() *sdkmcp.Server {
	s.build()
	return s.sdk
}

// adaptHandler bridges server.Handler to sdkmcp.ToolHandler.
// server.Handler: func(ctx, json.RawMessage) (string, error)
// sdkmcp.ToolHandler: func(ctx, *CallToolRequest) (*CallToolResult, error)
//
// Includes panic recovery — a panicking handler returns ErrorResult, not a crash.
func adaptHandler(h server.Handler) sdkmcp.ToolHandler {
	return func(ctx context.Context, req *sdkmcp.CallToolRequest) (res *sdkmcp.CallToolResult, retErr error) {
		defer func() {
			if r := recover(); r != nil {
				res = ErrorResult(fmt.Errorf("%w: %v", ErrHandlerPanicked, r))
				retErr = nil
			}
		}()

		var input json.RawMessage
		if req.Params != nil {
			input = req.Params.Arguments
		}

		result, err := h(ctx, input)
		if err != nil {
			return ErrorResult(err), nil
		}

		return TextResult(result), nil
	}
}
