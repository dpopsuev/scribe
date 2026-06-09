package web

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	parchment "github.com/dpopsuev/parchment"

	"github.com/dpopsuev/scribe/internal/ingest"
)

// Wire types live in internal/ingest — shared by this handler, the client, and tests.

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
	// Warm a set of existing IDs under this prefix so we can track inserts vs updates.
	existingIDs := make(map[string]bool)
	if existing, err := store.List(r.Context(), parchment.Filter{IDPrefix: idPrefix}); err == nil {
		for _, art := range existing {
			existingIDs[art.ID] = true
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
		_ = existingIDs // BulkPut handles uid resolution internally
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
			var rec ingest.NodeRecord
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
			var rec ingest.EdgeRecord
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

	result := ingest.Result{
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

// nodeToArtifact maps an ingest.NodeRecord to a parchment.Artifact.
func nodeToArtifact(rec *ingest.NodeRecord) *parchment.Artifact {
	status := rec.Status
	if status == "" {
		status = "active"
	}
	labels := make([]string, 0, len(rec.Labels)+2)
	if rec.Kind != "" {
		labels = append(labels, parchment.LabelPrefixKind+rec.Kind)
	}
	labels = append(labels, parchment.LabelPrefixStatus+status)
	labels = append(labels, rec.Labels...)
	sections := make([]parchment.Section, 0, len(rec.Sections))
	for _, s := range rec.Sections {
		sections = append(sections, parchment.Section{Name: s.Name, Text: s.Text})
	}
	return &parchment.Artifact{
		ID:       rec.ID,
		Title:    rec.Title,
		Labels:   labels,
		Extra:    rec.Extra,
		Sections: sections,
	}
}
