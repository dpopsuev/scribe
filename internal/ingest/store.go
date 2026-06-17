package ingest

import (
	"context"
	"fmt"
	"time"

	parchment "github.com/dpopsuev/parchment"
)

const batchSize = 200

// Apply writes nodes and edges into store. This is the abstraction both
// the HTTP handler and tests depend on — no transport required.
// If schemas is non-nil, Extra fields are validated against the source schema.
func Apply(ctx context.Context, store parchment.Store, source string, nodes []NodeRecord, edges []EdgeRecord, schemas ...map[string]parchment.SourceSchema) (*Result, error) {
	var sourceSchemas map[string]parchment.SourceSchema
	if len(schemas) > 0 {
		sourceSchemas = schemas[0]
	}
	start := time.Now()
	var inserted, edgesFailed int
	var errs []string

	idPrefix := source + ":"
	existingIDs := make(map[string]bool)
	if existing, err := store.List(ctx, parchment.Filter{IDPrefix: idPrefix}); err == nil {
		for _, art := range existing {
			existingIDs[art.ID] = true
		}
	}

	for i := 0; i < len(nodes); i += batchSize {
		end := i + batchSize
		if end > len(nodes) {
			end = len(nodes)
		}
		batch := make([]*parchment.Artifact, 0, end-i)
		for j := range nodes[i:end] {
			rec := &nodes[i+j]
			if sourceSchemas != nil {
				if validationErrs := parchment.ValidateExtra(sourceSchemas, source, rec.Extra); len(validationErrs) > 0 {
					for _, ve := range validationErrs {
						errs = append(errs, fmt.Sprintf("%s: %s", rec.ID, ve))
					}
					continue
				}
			}
			batch = append(batch, NodeToArtifact(rec))
		}
		for j, err := range store.BulkPut(ctx, batch) {
			if err != nil {
				errs = append(errs, fmt.Sprintf("%s: %v", batch[j].ID, err))
			} else {
				inserted++
			}
		}
	}

	pEdges := make([]parchment.Edge, 0, len(edges))
	for _, e := range edges {
		pEdges = append(pEdges, parchment.Edge{
			From: e.From, To: e.To, Relation: e.Relation, Weight: e.Weight,
		})
	}
	for i := 0; i < len(pEdges); i += batchSize {
		end := i + batchSize
		if end > len(pEdges) {
			end = len(pEdges)
		}
		if err := store.BulkAddEdge(ctx, pEdges[i:end]); err != nil {
			edgesFailed += end - i
			errs = append(errs, fmt.Sprintf("bulk edge insert: %v", err))
		}
	}

	return &Result{
		Inserted:    inserted,
		EdgesFailed: edgesFailed,
		Errors:      errs,
		Duration:    time.Since(start).String(),
	}, nil
}

// NodeToArtifact converts a NodeRecord to a parchment Artifact.
func NodeToArtifact(rec *NodeRecord) *parchment.Artifact {
	status := rec.Status
	if status == "" {
		status = "work.active"
	}
	labels := make([]string, 0, len(rec.Labels)+2)
	if rec.Kind != "" {
		labels = append(labels, parchment.LabelPrefixKind+rec.Kind)
	}
	if parchment.IsDomainStatusLabel(status) {
		labels = append(labels, status)
	} else {
		labels = append(labels, parchment.LabelPrefixStatus+status)
	}
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
