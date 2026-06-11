package integration_test

import (
	"context"
	"testing"
	"time"

	"github.com/dpopsuev/scribe/internal/ingest"
	"github.com/dpopsuev/scribe/testkit"
)

func TestIngest_Scale(t *testing.T) {
	db := testkit.NewStore(t)

	gen := &testkit.Generator{Source: "locus", NodeCount: 200, EdgesPerNode: 5, Shape: testkit.LocusComponentShape}
	nodes, edges := testkit.ParseGenerated(t, gen)

	start := time.Now()
	result, err := ingest.Apply(context.Background(), db, "locus", nodes, edges)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if len(result.Errors) > 0 {
		t.Errorf("errors: %v", result.Errors)
	}
	if result.Inserted != gen.NodeCount {
		t.Errorf("inserted=%d want %d", result.Inserted, gen.NodeCount)
	}
	if elapsed > 3*time.Second {
		t.Errorf("took %v, want < 3s", elapsed)
	}
	if got := testkit.CountByLabels(t, db, "source:locus"); got != gen.NodeCount {
		t.Errorf("parchment has %d nodes, want %d", got, gen.NodeCount)
	}
}

func TestIngest_Idempotency(t *testing.T) {
	db := testkit.NewStore(t)

	gen := &testkit.Generator{Source: "locus", NodeCount: 50, ScanSHA: "fixed-sha", Shape: testkit.LocusComponentShape}
	nodes, edges := testkit.ParseGenerated(t, gen)

	for i, label := range []string{"first", "second"} {
		r, err := ingest.Apply(context.Background(), db, "locus", nodes, edges)
		if err != nil || len(r.Errors) > 0 {
			t.Fatalf("run %d (%s): err=%v errors=%v", i+1, label, err, r.Errors)
		}
	}
	if got := testkit.CountByLabels(t, db, "source:locus"); got != gen.NodeCount {
		t.Errorf("after two runs: %d nodes, want %d", got, gen.NodeCount)
	}
}

func TestIngest_MultiSource(t *testing.T) {
	db := testkit.NewStore(t)

	cases := []struct {
		source string
		shape  testkit.ShapeFunc
		count  int
	}{
		{"locus", testkit.LocusComponentShape, 30},
		{"jira", testkit.JiraIssueShape, 20},
		{"github", testkit.GitHubPRShape, 10},
	}
	for _, c := range cases {
		gen := &testkit.Generator{Source: c.source, NodeCount: c.count, Shape: c.shape}
		nodes, edges := testkit.ParseGenerated(t, gen)
		r, err := ingest.Apply(context.Background(), db, c.source, nodes, edges)
		if err != nil || len(r.Errors) > 0 {
			t.Errorf("source=%s: err=%v errors=%v", c.source, err, r.Errors)
		}
		if got := testkit.CountByLabels(t, db, "source:"+c.source); got != c.count {
			t.Errorf("source=%s: %d nodes, want %d", c.source, got, c.count)
		}
	}
}
