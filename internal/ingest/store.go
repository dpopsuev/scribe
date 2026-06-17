package ingest

import (
	"context"
	"fmt"
	"time"

	parchment "github.com/dpopsuev/parchment"
)

const batchSize = 200

// Apply writes nodes and edges into store. If schemas is provided,
// Extra fields are validated against the source schema before insert.
func Apply(ctx context.Context, store parchment.Store, source string, nodes []NodeRecord, edges []EdgeRecord, schemas ...map[string]parchment.SourceSchema) (*Result, error) {
	r := &resultBuilder{start: time.Now()}
	validator := extraValidator(schemas)

	for _, batch := range batches(nodes, batchSize) {
		arts := r.convertBatch(batch, source, validator)
		for j, err := range store.BulkPut(ctx, arts) {
			if err != nil {
				r.recordError(arts[j].ID, err)
			} else {
				r.inserted++
			}
		}
	}

	pEdges := convertEdges(edges)
	for _, batch := range batchEdges(pEdges, batchSize) {
		if err := store.BulkAddEdge(ctx, batch); err != nil {
			r.edgesFailed += len(batch)
			r.errs = append(r.errs, fmt.Sprintf("bulk edge insert: %v", err))
		}
	}

	return r.result(), nil
}

type resultBuilder struct {
	start       time.Time
	inserted    int
	edgesFailed int
	errs        []string
}

func (r *resultBuilder) convertBatch(nodes []NodeRecord, source string, validate func(string, map[string]any) []string) []*parchment.Artifact {
	arts := make([]*parchment.Artifact, 0, len(nodes))
	for i := range nodes {
		if violations := validate(source, nodes[i].Extra); len(violations) > 0 {
			for _, v := range violations {
				r.recordError(nodes[i].ID, fmt.Errorf("%s", v)) //nolint:err113 // validation message
			}
			continue
		}
		arts = append(arts, NodeToArtifact(&nodes[i]))
	}
	return arts
}

func (r *resultBuilder) recordError(id string, err error) {
	r.errs = append(r.errs, fmt.Sprintf("%s: %v", id, err))
}

func (r *resultBuilder) result() *Result {
	return &Result{
		Inserted:    r.inserted,
		EdgesFailed: r.edgesFailed,
		Errors:      r.errs,
		Duration:    time.Since(r.start).String(),
	}
}

func extraValidator(schemas []map[string]parchment.SourceSchema) func(string, map[string]any) []string {
	if len(schemas) == 0 || schemas[0] == nil {
		return func(string, map[string]any) []string { return nil }
	}
	s := schemas[0]
	return func(source string, extra map[string]any) []string {
		return parchment.ValidateExtra(s, source, extra)
	}
}

func batches(nodes []NodeRecord, size int) [][]NodeRecord {
	var out [][]NodeRecord
	for i := 0; i < len(nodes); i += size {
		end := i + size
		if end > len(nodes) {
			end = len(nodes)
		}
		out = append(out, nodes[i:end])
	}
	return out
}

func batchEdges(edges []parchment.Edge, size int) [][]parchment.Edge {
	var out [][]parchment.Edge
	for i := 0; i < len(edges); i += size {
		end := i + size
		if end > len(edges) {
			end = len(edges)
		}
		out = append(out, edges[i:end])
	}
	return out
}

func convertEdges(edges []EdgeRecord) []parchment.Edge {
	out := make([]parchment.Edge, 0, len(edges))
	for _, e := range edges {
		out = append(out, parchment.Edge{
			From: e.From, To: e.To, Relation: e.Relation, Weight: e.Weight,
		})
	}
	return out
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
