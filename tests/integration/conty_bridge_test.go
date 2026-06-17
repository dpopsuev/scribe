//go:build integration

package integration_test

import (
	"context"
	"testing"

	contybridge "github.com/dpopsuev/conty/bridges/scribe"
	contytest "github.com/dpopsuev/conty/testdata"
	"github.com/dpopsuev/scribe/internal/ingest"
	"github.com/dpopsuev/scribe/testkit"
)

func TestContyBridge_BuildsIngest(t *testing.T) {
	db := testkit.NewStore(t)
	ctx := context.Background()

	builds := contytest.SampleBuilds()
	result := contybridge.TranslateBuilds(builds, "jenkins")

	nodes := recordsToNodes(result.Records)
	edges := edgesToRecords(result.Edges)

	res, err := ingest.Apply(ctx, db, "conty", nodes, edges)
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	if res.Inserted != 3 {
		t.Errorf("inserted = %d; want 3", res.Inserted)
	}

	count := testkit.CountByLabels(t, db, "source:conty")
	if count != 3 {
		t.Errorf("conty artifacts = %d; want 3", count)
	}
}

func TestContyBridge_BuildTreeIngest(t *testing.T) {
	db := testkit.NewStore(t)
	ctx := context.Background()

	tree := contytest.SampleBuildTree()
	result := contybridge.TranslateBuildTree(tree, "jenkins")

	nodes := recordsToNodes(result.Records)
	edges := edgesToRecords(result.Edges)

	res, err := ingest.Apply(ctx, db, "conty", nodes, edges)
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	if res.Inserted != 3 {
		t.Errorf("inserted = %d; want 3 (parent + 2 children)", res.Inserted)
	}

	count := testkit.CountByLabels(t, db, "backend:jenkins")
	if count != 3 {
		t.Errorf("jenkins artifacts = %d; want 3", count)
	}
}

func TestContyBridge_ResultLabels(t *testing.T) {
	builds := contytest.SampleBuilds()
	result := contybridge.TranslateBuilds(builds, "jenkins")

	success := result.Records[0]
	hasResult := false
	for _, l := range success.Labels {
		if l == "result:success" {
			hasResult = true
		}
	}
	if !hasResult {
		t.Errorf("missing result:success label; labels = %v", success.Labels)
	}

	failure := result.Records[1]
	hasFail := false
	for _, l := range failure.Labels {
		if l == "result:failure" {
			hasFail = true
		}
	}
	if !hasFail {
		t.Errorf("missing result:failure label; labels = %v", failure.Labels)
	}
}
