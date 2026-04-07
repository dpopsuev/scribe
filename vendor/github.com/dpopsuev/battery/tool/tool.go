// Package tool defines the atomic unit of agent capability.
// Tool is the interface. Executor dispatches by name.
package tool

import (
	"context"
	"encoding/json"
	"errors"
)

// Sentinel errors.
var (
	ErrNotFound   = errors.New("battery: tool not found")
	ErrEmptyInput = errors.New("battery: empty tool input")
)

// Tool is the interface every agent tool implements.
// Matches the MCP tool shape: name, description, JSON schema, execute.
type Tool interface {
	Name() string
	Description() string
	InputSchema() json.RawMessage
	Execute(ctx context.Context, input json.RawMessage) (string, error)
}

// Executor dispatches tool calls by name. Registry, Envelope, and
// Clearance all implement this interface (LSP — substitutable).
type Executor interface {
	Execute(ctx context.Context, name string, input json.RawMessage) (string, error)
	All() []Tool
	Names() []string
}
