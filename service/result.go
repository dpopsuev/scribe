//nolint:goconst // JSON schema keys are intentional literals
package service

// Result is the dual CLI/MCP return value for registry ops.
// Text is always populated for humans and the CLI.
// Data, when set, is returned as MCP structuredContent.
type Result struct {
	Text string `json:"-"`
	Data any    `json:"data,omitempty"`
}

// TextResult wraps a human-readable string with no structured payload.
func TextResult(text string) Result {
	return Result{Text: text}
}

// ArtifactRef is a machine-readable identity for a created or planned artifact.
type ArtifactRef struct {
	ID      string `json:"id"`
	Kind    string `json:"kind,omitempty"`
	Status  string `json:"status,omitempty"`
	Title   string `json:"title,omitempty"`
	Scope   string `json:"scope,omitempty"`
	TempRef string `json:"temp_ref,omitempty"`
	Parent  string `json:"parent,omitempty"`
}

// EdgeRef is a planned or applied graph edge.
type EdgeRef struct {
	From     string  `json:"from"`
	Relation string  `json:"relation"`
	To       string  `json:"to"`
	Weight   float64 `json:"weight,omitempty"`
}

// MutationResult is the typed contract for create/plan/apply and other writes.
type MutationResult struct {
	Action     string        `json:"action"`
	Status     string        `json:"status"` // planned | applied | dry_run | ok | error
	MutationID string        `json:"mutation_id,omitempty"`
	DryRun     bool          `json:"dry_run,omitempty"`
	Artifacts  []ArtifactRef `json:"artifacts,omitempty"`
	Edges      []EdgeRef     `json:"edges,omitempty"`
	Warnings   []string      `json:"warnings,omitempty"`
	IDs        []string      `json:"ids,omitempty"`
	Count      int           `json:"count,omitempty"`
	RolledBack bool          `json:"rolled_back,omitempty"`
	Idempotent bool          `json:"idempotent,omitempty"`
}

// MutationOutputSchema is the JSON Schema object advertised on the artifact tool.
func MutationOutputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action":      map[string]any{"type": "string"},
			"status":      map[string]any{"type": "string"},
			"mutation_id": map[string]any{"type": "string"},
			"dry_run":     map[string]any{"type": "boolean"},
			"artifacts": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"id":       map[string]any{"type": "string"},
						"kind":     map[string]any{"type": "string"},
						"status":   map[string]any{"type": "string"},
						"title":    map[string]any{"type": "string"},
						"scope":    map[string]any{"type": "string"},
						"temp_ref": map[string]any{"type": "string"},
						"parent":   map[string]any{"type": "string"},
					},
					"required": []string{"id"},
				},
			},
			"edges": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"from":     map[string]any{"type": "string"},
						"relation": map[string]any{"type": "string"},
						"to":       map[string]any{"type": "string"},
					},
					"required": []string{"from", "relation", "to"},
				},
			},
			"warnings":    map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"ids":         map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"count":       map[string]any{"type": "integer"},
			"rolled_back": map[string]any{"type": "boolean"},
			"idempotent":  map[string]any{"type": "boolean"},
		},
		"required": []string{"action", "status"},
	}
}
