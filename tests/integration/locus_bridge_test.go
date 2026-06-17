package integration_test

import (
	"context"
	"testing"

	"github.com/dpopsuev/battery/translate"
	scribebridge "github.com/dpopsuev/locus/bridges/scribe"
	locustest "github.com/dpopsuev/locus/testdata"
	parchment "github.com/dpopsuev/parchment"
	"github.com/dpopsuev/scribe/internal/ingest"
	"github.com/dpopsuev/scribe/testkit"
)

func recordsToNodes(recs []translate.Record) []ingest.NodeRecord {
	nodes := make([]ingest.NodeRecord, len(recs))
	for i, r := range recs {
		sections := make([]ingest.Section, len(r.Sections))
		for j, s := range r.Sections {
			sections[j] = ingest.Section{Name: s.Name, Text: s.Text}
		}
		nodes[i] = ingest.NodeRecord{
			Type:     "node",
			ID:       r.ID,
			Kind:     r.Kind,
			Title:    r.Title,
			Labels:   r.Labels,
			Extra:    r.Extra,
			Sections: sections,
		}
	}
	return nodes
}

func edgesToRecords(edges []translate.Edge) []ingest.EdgeRecord {
	recs := make([]ingest.EdgeRecord, len(edges))
	for i, e := range edges {
		recs[i] = ingest.EdgeRecord{
			Type:     "edge",
			From:     e.From,
			To:       e.To,
			Relation: e.Relation,
		}
	}
	return recs
}

func TestLocusBridge_SmallProject(t *testing.T) {
	db := testkit.NewStore(t)
	ctx := context.Background()

	report := locustest.SmallProject()
	result := scribebridge.TranslateScan(report, "test-small")

	nodes := recordsToNodes(result.Records)
	edges := edgesToRecords(result.Edges)

	res, err := ingest.Apply(ctx, db, "locus", nodes, edges)
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	if res.Inserted != 3 {
		t.Errorf("inserted = %d; want 3", res.Inserted)
	}
	if len(res.Errors) > 0 {
		t.Errorf("errors: %v", res.Errors)
	}

	count := testkit.CountByLabels(t, db, "source:locus")
	if count != 3 {
		t.Errorf("locus artifacts = %d; want 3", count)
	}

	arts, err := db.List(ctx, parchment.Filter{IDPrefix: "test-small/"})
	if err != nil {
		t.Fatal(err)
	}
	if len(arts) != 3 {
		t.Fatalf("artifacts = %d; want 3", len(arts))
	}

	names := make(map[string]bool)
	for _, a := range arts {
		names[a.Title] = true
	}
	for _, want := range []string{"api", "service", "db"} {
		if !names[want] {
			t.Errorf("missing component %q", want)
		}
	}
}

func TestLocusBridge_MonorepoProject(t *testing.T) {
	db := testkit.NewStore(t)
	ctx := context.Background()

	report := locustest.MonorepoProject()
	result := scribebridge.TranslateScan(report, "test-mono")

	nodes := recordsToNodes(result.Records)
	edges := edgesToRecords(result.Edges)

	res, err := ingest.Apply(ctx, db, "locus", nodes, edges)
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	if res.Inserted != 6 {
		t.Errorf("inserted = %d; want 6", res.Inserted)
	}

	count := testkit.CountByLabels(t, db, "source:locus", "project:test-mono")
	if count != 6 {
		t.Errorf("locus mono artifacts = %d; want 6", count)
	}
}

func TestLocusBridge_Idempotent(t *testing.T) {
	db := testkit.NewStore(t)
	ctx := context.Background()

	report := locustest.SmallProject()
	result := scribebridge.TranslateScan(report, "test-idem")

	nodes := recordsToNodes(result.Records)
	edges := edgesToRecords(result.Edges)

	res1, _ := ingest.Apply(ctx, db, "locus", nodes, edges)
	res2, _ := ingest.Apply(ctx, db, "locus", nodes, edges)

	if res1.Inserted != 3 {
		t.Errorf("first ingest = %d; want 3", res1.Inserted)
	}

	total := testkit.CountByLabels(t, db, "source:locus")
	if total != 3 {
		t.Errorf("after double ingest = %d; want 3 (idempotent)", total)
	}
	_ = res2
}
