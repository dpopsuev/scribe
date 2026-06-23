package service

import (
	"context"

	"github.com/dpopsuev/battery/translate"
	parchment "github.com/dpopsuev/parchment"
	"github.com/dpopsuev/scribe/internal/ingest"
)

// ScribeSink implements materialize.Sink by converting translate.Result
// to ingest records and persisting via ingest.Apply.
type ScribeSink struct {
	store parchment.Store
}

// NewScribeSink creates a Sink backed by a parchment Store.
func NewScribeSink(store parchment.Store) *ScribeSink {
	return &ScribeSink{store: store}
}

// Push converts translate.Records to ingest.NodeRecords and persists them.
func (s *ScribeSink) Push(ctx context.Context, source string, result translate.Result) error {
	nodes := make([]ingest.NodeRecord, len(result.Records))
	for i, r := range result.Records {
		nodes[i] = ingest.NodeRecord{
			Type:   "node",
			ID:     r.ID,
			Kind:   r.Kind,
			Title:  r.Title,
			Labels: r.Labels,
			Extra:  r.Extra,
		}
		for _, sec := range r.Sections {
			nodes[i].Sections = append(nodes[i].Sections, ingest.Section{
				Name: sec.Name,
				Text: sec.Text,
			})
		}
	}

	edges := make([]ingest.EdgeRecord, len(result.Edges))
	for i, e := range result.Edges {
		edges[i] = ingest.EdgeRecord{
			Type:     "edge",
			From:     e.From,
			To:       e.To,
			Relation: e.Relation,
		}
	}

	_, err := ingest.Apply(ctx, s.store, source, nodes, edges)
	return err
}
