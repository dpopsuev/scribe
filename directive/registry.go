package directive

import (
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// ToolMeta is the single source of truth for a Scribe tool. It drives both
// MCP registration (Name, Description) and CLI help output (Keywords, Categories).
type ToolMeta struct {
	Name        string
	Description string
	Keywords    []string
	Categories  []string
}

// Registry holds all registered tools.
type Registry struct {
	tools []ToolMeta
}

// New creates an empty Registry.
func New() *Registry {
	return &Registry{}
}

// Register adds a tool to the registry.
func (r *Registry) Register(meta ToolMeta) {
	r.tools = append(r.tools, meta)
}

// AddTool registers a tool for both MCP serving and the directive registry.
// This is the converged registration point — one call replaces separate
// sdkmcp.AddTool + registry.Register calls.
func AddTool[In any](r *Registry, srv *sdkmcp.Server, meta ToolMeta, handler sdkmcp.ToolHandlerFor[In, any]) {
	sdkmcp.AddTool(srv, &sdkmcp.Tool{
		Name:        meta.Name,
		Description: meta.Description,
	}, handler)
	r.Register(meta)
}

// List returns a copy of all registered tools.
func (r *Registry) List() []ToolMeta {
	out := make([]ToolMeta, len(r.tools))
	copy(out, r.tools)
	return out
}

// ByCategory returns tools that belong to the given category.
func (r *Registry) ByCategory(cat string) []ToolMeta {
	var out []ToolMeta
	for _, t := range r.tools {
		for _, c := range t.Categories {
			if c == cat {
				out = append(out, t)
				break
			}
		}
	}
	return out
}
