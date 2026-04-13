package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
)

// ErrServerToolNotFound is returned when a tool is not registered in the server registry.
var ErrServerToolNotFound = errors.New("battery: server tool not found")

type registeredTool struct {
	meta    ToolMeta
	handler Handler
}

// Registry holds server-side tools with enriched metadata.
type Registry struct {
	tools map[string]registeredTool
}

// NewRegistry creates an empty server tool registry.
func NewRegistry() *Registry {
	return &Registry{tools: make(map[string]registeredTool)}
}

// Add registers a tool handler with metadata.
func (r *Registry) Add(meta ToolMeta, h Handler) {
	r.tools[meta.Name] = registeredTool{meta: meta, handler: h}
}

// Handle dispatches a tool call by name.
func (r *Registry) Handle(ctx context.Context, name string, input json.RawMessage) (string, error) {
	t, ok := r.tools[name]
	if !ok {
		return "", fmt.Errorf("%w: %s", ErrServerToolNotFound, name)
	}
	return t.handler(ctx, input)
}

// List returns metadata for all registered tools, sorted by name.
func (r *Registry) List() []ToolMeta {
	out := make([]ToolMeta, 0, len(r.tools))
	for _, t := range r.tools {
		out = append(out, t.meta)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
