package integration_test

import (
	"sort"
	"testing"

	"github.com/dpopsuev/battery/translate"
	scribebridge "github.com/dpopsuev/locus/bridges/scribe"
	locustest "github.com/dpopsuev/locus/testdata"
	parchment "github.com/dpopsuev/parchment"
)

func TestLocusBridgeContract(t *testing.T) {
	report, sg := locustest.HexagonalProject()
	result := scribebridge.TranslateScanWithSymbols(report, sg, "hex")
	schema := parchment.DefaultSchema()

	wantRecords := []expectedRecord{
		// Components
		{ID: "hex/domain", Kind: "knowledge.source", Title: "domain"},
		{ID: "hex/adapter", Kind: "knowledge.source", Title: "adapter"},
		// Files
		{ID: "hex/domain:repo.go", Kind: "code.file", Title: "repo.go"},
		{ID: "hex/domain:svc.go", Kind: "code.file", Title: "svc.go"},
		{ID: "hex/adapter:pg.go", Kind: "code.file", Title: "pg.go"},
		// Symbols
		{ID: "hex/domain:repository", Kind: "code.interface", Title: "Repository"},
		{ID: "hex/domain:service", Kind: "code.struct", Title: "Service"},
		{ID: "hex/domain:service.create", Kind: "code.method", Title: "Service.Create"},
		{ID: "hex/adapter:pgrepo", Kind: "code.struct", Title: "PgRepo"},
	}

	wantEdges := []expectedEdge{
		// Component dependency
		{From: "hex/adapter", Relation: "depends_on", To: "hex/domain"},
		// Component → File
		{From: "hex/domain", Relation: "contains", To: "hex/domain:repo.go"},
		{From: "hex/domain", Relation: "contains", To: "hex/domain:svc.go"},
		{From: "hex/adapter", Relation: "contains", To: "hex/adapter:pg.go"},
		// File → Symbol
		{From: "hex/domain:repo.go", Relation: "contains", To: "hex/domain:repository"},
		{From: "hex/domain:svc.go", Relation: "contains", To: "hex/domain:service"},
		{From: "hex/domain:svc.go", Relation: "contains", To: "hex/domain:service.create"},
		{From: "hex/adapter:pg.go", Relation: "contains", To: "hex/adapter:pgrepo"},
		// Symbol → Symbol
		{From: "hex/adapter:pgrepo", Relation: "implements", To: "hex/domain:repository"},
		{From: "hex/domain:service", Relation: "field_ref", To: "hex/domain:repository"},
		{From: "hex/domain:service.create", Relation: "calls", To: "hex/adapter:pgrepo"},
		{From: "hex/domain:service", Relation: "embeds", To: "hex/domain:repository"},
	}

	// Verify record count and content.
	if len(result.Records) != len(wantRecords) {
		t.Fatalf("records = %d; want %d", len(result.Records), len(wantRecords))
	}
	gotRecs := toRecordSet(result.Records)
	for _, w := range wantRecords {
		r, ok := gotRecs[w.ID]
		if !ok {
			t.Errorf("missing record %s", w.ID)
			continue
		}
		if r.Kind != w.Kind {
			t.Errorf("record %s kind = %q; want %q", w.ID, r.Kind, w.Kind)
		}
		if r.Title != w.Title {
			t.Errorf("record %s title = %q; want %q", w.ID, r.Title, w.Title)
		}
	}

	// Verify edge count and content.
	if len(result.Edges) != len(wantEdges) {
		t.Fatalf("edges = %d; want %d\ngot: %v", len(result.Edges), len(wantEdges), sortedEdges(result.Edges))
	}
	gotEdges := toEdgeSet(result.Edges)
	for _, w := range wantEdges {
		key := w.From + "|" + w.Relation + "|" + w.To
		if !gotEdges[key] {
			t.Errorf("missing edge %s --%s--> %s", w.From, w.Relation, w.To)
		}
	}

	// Every relation in the output must be valid in Parchment's schema.
	for _, e := range result.Edges {
		if !schema.ValidRelation(e.Relation) {
			t.Errorf("edge %s→%s: relation %q not in schema", e.From, e.To, e.Relation)
		}
	}

	// Verify layer_depth on component records.
	domain := gotRecs["hex/domain"]
	if domain.Extra["layer_depth"] != 1 {
		t.Errorf("domain layer_depth = %v; want 1", domain.Extra["layer_depth"])
	}
	adapter := gotRecs["hex/adapter"]
	if adapter.Extra["layer_depth"] != 0 {
		t.Errorf("adapter layer_depth = %v; want 0", adapter.Extra["layer_depth"])
	}
}

type expectedRecord struct {
	ID, Kind, Title string
}

type expectedEdge struct {
	From, Relation, To string
}

func toRecordSet(recs []translate.Record) map[string]translate.Record {
	m := make(map[string]translate.Record, len(recs))
	for _, r := range recs {
		m[r.ID] = r
	}
	return m
}

func toEdgeSet(edges []translate.Edge) map[string]bool {
	m := make(map[string]bool, len(edges))
	for _, e := range edges {
		m[e.From+"|"+e.Relation+"|"+e.To] = true
	}
	return m
}

func sortedEdges(edges []translate.Edge) []string {
	out := make([]string, len(edges))
	for i, e := range edges {
		out[i] = e.From + " --" + e.Relation + "--> " + e.To
	}
	sort.Strings(out)
	return out
}
