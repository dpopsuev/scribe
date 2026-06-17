//go:build integration

package integration_test

import (
	"context"
	"testing"

	"github.com/dpopsuev/battery/translate"
	emceebridge "github.com/dpopsuev/emcee/bridges/scribe"
	emceetest "github.com/dpopsuev/emcee/testdata"
	"github.com/dpopsuev/scribe/internal/ingest"
	"github.com/dpopsuev/scribe/testkit"
)

func TestEmceeBridge_IssuesIngest(t *testing.T) {
	db := testkit.NewStore(t)
	ctx := context.Background()

	issues := emceetest.SampleIssues()
	result := emceebridge.TranslateIssues(issues)

	nodes := recordsToNodes(result.Records)
	edges := edgesToRecords(result.Edges)

	res, err := ingest.Apply(ctx, db, "emcee", nodes, edges)
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	if res.Inserted != 3 {
		t.Errorf("inserted = %d; want 3", res.Inserted)
	}

	count := testkit.CountByLabels(t, db, "source:emcee")
	if count != 3 {
		t.Errorf("emcee artifacts = %d; want 3", count)
	}
}

func TestEmceeBridge_TriageGraphIngest(t *testing.T) {
	db := testkit.NewStore(t)
	ctx := context.Background()

	graph := emceetest.SampleTriageGraph()
	result := emceebridge.TranslateTriageGraph(graph)

	nodes := recordsToNodes(result.Records)
	edges := edgesToRecords(result.Edges)

	res, err := ingest.Apply(ctx, db, "emcee", nodes, edges)
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	if res.Inserted != 3 {
		t.Errorf("inserted = %d; want 3 (issue + PR + build)", res.Inserted)
	}

	count := testkit.CountByLabels(t, db, "triage:jira:AUTH-42")
	if count != 3 {
		t.Errorf("triage artifacts = %d; want 3", count)
	}
}

func TestEmceeBridge_IssueKindMapping(t *testing.T) {
	issues := emceetest.SampleIssues()
	result := emceebridge.TranslateIssues(issues)

	expected := map[string]string{
		"jira:AUTH-42":       "intent.bug",
		"jira:AUTH-43":       "intent.spec",
		"github:org/api#127": "intent.bug",
	}

	for _, r := range result.Records {
		want, ok := expected[r.ID]
		if !ok {
			t.Errorf("unexpected record %q", r.ID)
			continue
		}
		if r.Kind != want {
			t.Errorf("%s kind = %q; want %q", r.ID, r.Kind, want)
		}
	}
}

// Suppress unused import warning — translate types used via recordsToNodes/edgesToRecords.
var _ translate.Record
