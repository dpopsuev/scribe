package testkit_test

import (
	"testing"

	"github.com/dpopsuev/scribe/testkit"
)

func TestDevOpsLoopGenerator(t *testing.T) {
	gen := &testkit.DevOpsLoopGenerator{Project: "ptp", Source: "sim"}
	nodes, edges := gen.Generate()

	if len(nodes) == 0 {
		t.Fatal("expected nodes from DevOps loop generator")
	}
	if len(edges) == 0 {
		t.Fatal("expected cross-phase edges")
	}

	phases := make(map[string]int)
	for _, n := range nodes {
		for _, l := range n.Labels {
			if len(l) > 6 && l[:6] == "phase:" {
				phases[l[6:]]++
			}
		}
	}

	expectedPhases := []string{"plan", "code", "commit", "build", "test", "release", "deploy", "operate", "monitor"}
	for _, p := range expectedPhases {
		if phases[p] == 0 {
			t.Errorf("missing phase %q in generated nodes", p)
		}
	}

	projectFound := false
	for _, n := range nodes {
		for _, l := range n.Labels {
			if l == "project:ptp" {
				projectFound = true
				break
			}
		}
		if projectFound {
			break
		}
	}
	if !projectFound {
		t.Error("expected project:ptp label on nodes")
	}

	t.Logf("Generated %d nodes across %d phases, %d cross-phase edges", len(nodes), len(phases), len(edges))
}

func TestAllShapesProduceValidRecords(t *testing.T) {
	shapes := map[string]testkit.ShapeFunc{
		"GitCommit":        testkit.GitCommitShape,
		"CIBuild":          testkit.CIBuildShape,
		"TestResult":       testkit.TestResultShape,
		"Release":          testkit.ReleaseShape,
		"Deployment":       testkit.DeploymentShape,
		"OperationalEvent": testkit.OperationalEventShape,
		"Alert":            testkit.AlertShape,
		"LocusComponent":   testkit.LocusComponentShape,
		"JiraIssue":        testkit.JiraIssueShape,
		"GitHubPR":         testkit.GitHubPRShape,
	}

	for name, shape := range shapes {
		t.Run(name, func(t *testing.T) {
			rec := shape(0, "test-source", "abc123")
			if rec.ID == "" {
				t.Error("empty ID")
			}
			if rec.Title == "" {
				t.Error("empty Title")
			}
			if len(rec.Labels) == 0 {
				t.Error("no labels")
			}
		})
	}
}
