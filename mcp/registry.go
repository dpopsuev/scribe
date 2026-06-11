package mcp

type ToolMeta struct {
	Name        string
	Description string
	Keywords    []string
	Categories  []string
}

type Registry struct {
	tools []ToolMeta
}

func newRegistry() *Registry {
	return &Registry{}
}

func (r *Registry) register(meta ToolMeta) {
	r.tools = append(r.tools, meta)
}

func (r *Registry) List() []ToolMeta {
	out := make([]ToolMeta, len(r.tools))
	copy(out, r.tools)
	return out
}

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
