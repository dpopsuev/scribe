// Package server provides MCP server toolkit: tool registry with enriched
// metadata, observable handler wrapping, and role-based clearance filtering.
package server

import (
	"context"
	"encoding/json"
)

// ToolMeta is enriched metadata beyond tool.Tool — keywords, categories, priority.
type ToolMeta struct {
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Keywords    []string          `json:"keywords,omitempty"`
	Categories  []string          `json:"categories,omitempty"`
	Priority    int               `json:"priority,omitempty"`
	DefaultArgs map[string]any    `json:"default_args,omitempty"`
	Rationale   map[string]string `json:"rationale,omitempty"` // category → why this tool matters
}

// Handler is a server-side tool handler.
type Handler func(ctx context.Context, input json.RawMessage) (string, error)

// Result wraps a tool execution result.
type Result struct {
	Content string `json:"content"`
	IsError bool   `json:"is_error,omitempty"`
}
