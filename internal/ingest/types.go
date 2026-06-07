// Package ingest defines the NDJSON wire protocol for streaming graph data
// into Scribe's POST /api/v1/ingest endpoint.
//
// Each line in the stream is one JSON object with a "type" discriminator:
//
//	{"type":"node", "id":"locus:component:web", "kind":"note", ...}
//	{"type":"edge", "from":"...", "to":"...", "relation":"imports"}
//	{"type":"meta", "source":"locus", "scan_sha":"abc", ...}
//
// The same types are used by the server (web/ingest.go), the client
// (internal/ingest/client.go), and the test generator (internal/ingest/generator.go).
package ingest

// NodeRecord is a node to upsert into parchment.
type NodeRecord struct {
	Type     string         `json:"type"` // "node"
	ID       string         `json:"id"`
	Kind     string         `json:"kind"`
	Title    string         `json:"title"`
	Labels   []string       `json:"labels,omitempty"`
	Extra    map[string]any `json:"extra,omitempty"`
	Sections []Section      `json:"sections,omitempty"`
	Status   string         `json:"status,omitempty"`
}

// Section is a named free-text block on a node.
type Section struct {
	Name string `json:"name"`
	Text string `json:"text"`
}

// EdgeRecord is a directed relationship between two nodes.
type EdgeRecord struct {
	Type     string  `json:"type"` // "edge"
	From     string  `json:"from"`
	To       string  `json:"to"`
	Relation string  `json:"relation"`
	Weight   float64 `json:"weight,omitempty"`
}

// MetaRecord closes the stream and carries provenance for stale detection.
type MetaRecord struct {
	Type       string `json:"type"`   // "meta"
	Source     string `json:"source"` // e.g. "locus", "jira", "github"
	ScanSHA    string `json:"scan_sha,omitempty"`
	ScannedAt  string `json:"scanned_at,omitempty"`
	TotalNodes int    `json:"total_nodes,omitempty"`
	TotalEdges int    `json:"total_edges,omitempty"`
}

// Result is returned by the server after consuming the full stream.
type Result struct {
	Inserted    int      `json:"inserted"`
	Updated     int      `json:"updated,omitempty"`
	EdgesFailed int      `json:"edges_failed,omitempty"`
	Errors      []string `json:"errors,omitempty"`
	Duration    string   `json:"duration"`
}
