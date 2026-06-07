package web

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	parchment "github.com/dpopsuev/parchment"
)

// ── NDJSON wire types ─────────────────────────────────────────────────────
// Each line in the stream is one of these three record types, discriminated
// by the "type" field. Producers (e.g. locus scan --format ndjson) emit
// nodes and edges interleaved; the meta record marks end-of-stream.

// IngestNodeRecord is a node to upsert into parchment.
// Maps 1:1 to parchment.Artifact — extra fields are stored in Artifact.Extra.
type IngestNodeRecord struct {
	Type     string          `json:"type"` // "node"
	ID       string          `json:"id"`
	Kind     string          `json:"kind"`
	Title    string          `json:"title"`
	Labels   []string        `json:"labels,omitempty"`
	Extra    map[string]any  `json:"extra,omitempty"`
	Sections []ingestSection `json:"sections,omitempty"`
	Status   string          `json:"status,omitempty"`
}

type ingestSection struct {
	Name string `json:"name"`
	Text string `json:"text"`
}

// IngestEdgeRecord is a directed edge to create between two nodes.
type IngestEdgeRecord struct {
	Type     string  `json:"type"` // "edge"
	From     string  `json:"from"`
	To       string  `json:"to"`
	Relation string  `json:"relation"`
	Weight   float64 `json:"weight,omitempty"`
}

// IngestMetaRecord closes the stream and carries provenance for stale detection.
type IngestMetaRecord struct {
	Type       string `json:"type"`   // "meta"
	Source     string `json:"source"` // e.g. "locus"
	ScanSHA    string `json:"scan_sha,omitempty"`
	ScannedAt  string `json:"scanned_at,omitempty"`
	TotalNodes int    `json:"total_nodes,omitempty"`
	TotalEdges int    `json:"total_edges,omitempty"`
}

// IngestResult is returned as JSON once the stream is fully consumed.
type IngestResult struct {
	Inserted    int      `json:"inserted"`
	Updated     int      `json:"updated"`
	EdgesFailed int      `json:"edges_failed,omitempty"`
	Errors      []string `json:"errors,omitempty"`
	Duration    string   `json:"duration"`
}

const ingestBatchSize = 200 // records per BulkPut transaction

// handleAPIIngest reads an NDJSON stream of IngestNodeRecord / IngestEdgeRecord
// / IngestMetaRecord from the request body and upserts them into parchment.
//
// POST /api/v1/ingest
// Content-Type: application/x-ndjson
//
// Partial writes succeed: if the connection drops mid-stream, everything
// committed so far is in parchment. The next run is idempotent (same ID →
// upsert, not duplicate).
func (s *Server) handleAPIIngest(w http.ResponseWriter, r *http.Request) { //nolint:gocyclo // streaming ingest: dispatch on record type is inherently branchy; splitting adds indirection without clarity
	start := time.Now()
	dec := json.NewDecoder(r.Body)

	var (
		batch       []*parchment.Artifact
		edgeBatch   []parchment.Edge
		inserted    int
		updated     int
		edgesFailed int
		errs        []string
	)

	store := s.proto.Store()

	// Pre-fetch all existing nodes from this source in one List call.
	// Builds an id→UID map used by flush() to populate UIDs so BulkPut's
	// ON CONFLICT(uid) path updates instead of duplicating.
	// Using IDPrefix avoids N+1 per-node Get calls.
	source := r.URL.Query().Get("source")
	if source == "" {
		source = "unknown"
	}
	idPrefix := source + ":"
	existingUID := make(map[string]string) // id → uid
	if existing, err := store.List(r.Context(), parchment.Filter{IDPrefix: idPrefix}); err == nil {
		for _, art := range existing {
			existingUID[art.ID] = art.UID
		}
	}

	flushEdges := func() {
		if len(edgeBatch) == 0 {
			return
		}
		if err := store.BulkAddEdge(r.Context(), edgeBatch); err != nil {
			edgesFailed += len(edgeBatch)
			errs = append(errs, fmt.Sprintf("bulk edge insert: %v", err))
		}
		edgeBatch = edgeBatch[:0]
	}

	flush := func() {
		if len(batch) == 0 {
			return
		}
		for _, art := range batch {
			if art.UID == "" {
				art.UID = existingUID[art.ID] // empty string → new insert; non-empty → update
			}
		}
		results := store.BulkPut(r.Context(), batch)
		for i, err := range results {
			if err != nil {
				errs = append(errs, fmt.Sprintf("%s: %v", batch[i].ID, err))
			} else {
				inserted++
			}
		}
		batch = batch[:0]
	}

	for {
		// Peek at the "type" field to discriminate record kind.
		var raw json.RawMessage
		if err := dec.Decode(&raw); err == io.EOF {
			break
		} else if err != nil {
			errs = append(errs, fmt.Sprintf("decode error: %v", err))
			break
		}

		var typed struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(raw, &typed); err != nil {
			errs = append(errs, fmt.Sprintf("type field missing: %v", err))
			continue
		}

		switch typed.Type {
		case "node":
			var rec IngestNodeRecord
			if err := json.Unmarshal(raw, &rec); err != nil {
				errs = append(errs, fmt.Sprintf("bad node record: %v", err))
				continue
			}
			art := nodeToArtifact(&rec)
			batch = append(batch, art)
			if len(batch) >= ingestBatchSize {
				flush()
			}

		case "edge":
			var rec IngestEdgeRecord
			if err := json.Unmarshal(raw, &rec); err != nil {
				errs = append(errs, fmt.Sprintf("bad edge record: %v", err))
				continue
			}
			edgeBatch = append(edgeBatch, parchment.Edge{
				From:     rec.From,
				To:       rec.To,
				Relation: rec.Relation,
				Weight:   rec.Weight,
			})

		case "meta":
			// Meta marks end-of-stream; flush remaining nodes and edges.
			flush()
			flushEdges()

		default:
			errs = append(errs, fmt.Sprintf("unknown record type %q", typed.Type))
		}
	}

	flush()      // final node flush if stream ended without a meta record
	flushEdges() // final edge flush

	_ = updated // reserved for future diff-based upsert counting

	result := IngestResult{
		Inserted:    inserted,
		EdgesFailed: edgesFailed,
		Errors:      errs,
		Duration:    time.Since(start).String(),
	}
	w.Header().Set("Content-Type", "application/json")
	if len(errs) > 0 {
		w.WriteHeader(http.StatusMultiStatus)
	}
	_ = json.NewEncoder(w).Encode(result)
}

// nodeToArtifact maps an IngestNodeRecord to a parchment.Artifact.
func nodeToArtifact(rec *IngestNodeRecord) *parchment.Artifact {
	status := rec.Status
	if status == "" {
		status = "active"
	}
	sections := make([]parchment.Section, 0, len(rec.Sections))
	for _, s := range rec.Sections {
		sections = append(sections, parchment.Section{Name: s.Name, Text: s.Text})
	}
	return &parchment.Artifact{
		ID:       rec.ID,
		Kind:     rec.Kind,
		Title:    rec.Title,
		Status:   status,
		Labels:   rec.Labels,
		Extra:    rec.Extra,
		Sections: sections,
	}
}
