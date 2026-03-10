package protocol_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dpopsuev/scribe/model"
	"github.com/dpopsuev/scribe/protocol"
	"github.com/dpopsuev/scribe/store"
)

func openStore(t *testing.T) *store.SQLiteStore {
	t.Helper()
	s, err := store.OpenSQLite(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func newProto(t *testing.T) (*protocol.Protocol, store.Store) {
	t.Helper()
	s := openStore(t)
	return protocol.New(s, nil, nil, nil), s
}

func TestIsComponentLabel(t *testing.T) {
	good := []string{
		"locus:internal/arch",
		"scribe:protocol/protocol.go",
		"limes:internal/limesfile/limesfile.go",
		"my-project:src/pkg/foo",
		"a1:x/y",
	}
	bad := []string{
		"no-colon-here",
		"UPPER:path/foo",
		":path/foo",
		"project:",
		"project:noSlash",
		"",
	}
	for _, l := range good {
		if !protocol.IsComponentLabel(l) {
			t.Errorf("expected %q to be a valid component label", l)
		}
	}
	for _, l := range bad {
		if protocol.IsComponentLabel(l) {
			t.Errorf("expected %q to NOT be a valid component label", l)
		}
	}
}

func TestDetectOverlaps_NoOverlaps(t *testing.T) {
	p, s := newProto(t)
	ctx := context.Background()

	_ = s.Put(ctx, &model.Artifact{
		ID: "TASK-1", Kind: "task", Status: "active", Title: "A",
		Labels: []string{"locus:internal/arch"},
	})
	_ = s.Put(ctx, &model.Artifact{
		ID: "TASK-2", Kind: "task", Status: "active", Title: "B",
		Labels: []string{"locus:internal/mcp"},
	})

	report, err := p.DetectOverlaps(ctx, protocol.OverlapInput{})
	if err != nil {
		t.Fatal(err)
	}
	if report.TotalOverlaps != 0 {
		t.Errorf("expected 0 overlaps, got %d", report.TotalOverlaps)
	}
	if report.TotalScanned != 2 {
		t.Errorf("expected 2 scanned, got %d", report.TotalScanned)
	}
}

func TestDetectOverlaps_WithOverlaps(t *testing.T) {
	p, s := newProto(t)
	ctx := context.Background()

	_ = s.Put(ctx, &model.Artifact{
		ID: "TASK-1", Kind: "task", Status: "active", Title: "Task A",
		Labels: []string{"locus:internal/arch", "locus:internal/mcp"},
	})
	_ = s.Put(ctx, &model.Artifact{
		ID: "TASK-2", Kind: "task", Status: "active", Title: "Task B",
		Labels: []string{"locus:internal/arch"},
	})
	_ = s.Put(ctx, &model.Artifact{
		ID: "TASK-3", Kind: "task", Status: "active", Title: "Task C",
		Labels: []string{"scribe:protocol/protocol.go"},
	})

	report, err := p.DetectOverlaps(ctx, protocol.OverlapInput{})
	if err != nil {
		t.Fatal(err)
	}
	if report.TotalOverlaps != 1 {
		t.Errorf("expected 1 overlap, got %d", report.TotalOverlaps)
	}
	if report.TotalScanned != 3 {
		t.Errorf("expected 3 scanned, got %d", report.TotalScanned)
	}
	if report.Overlaps[0].Label != "locus:internal/arch" {
		t.Errorf("expected overlapping label locus:internal/arch, got %s", report.Overlaps[0].Label)
	}
	if len(report.Overlaps[0].Artifacts) != 2 {
		t.Errorf("expected 2 artifacts in overlap, got %d", len(report.Overlaps[0].Artifacts))
	}
}

func TestDetectOverlaps_ProjectFilter(t *testing.T) {
	p, s := newProto(t)
	ctx := context.Background()

	_ = s.Put(ctx, &model.Artifact{
		ID: "TASK-1", Kind: "task", Status: "active", Title: "A",
		Labels: []string{"locus:internal/arch", "scribe:mcp/server.go"},
	})
	_ = s.Put(ctx, &model.Artifact{
		ID: "TASK-2", Kind: "task", Status: "active", Title: "B",
		Labels: []string{"locus:internal/arch", "scribe:mcp/server.go"},
	})

	report, err := p.DetectOverlaps(ctx, protocol.OverlapInput{Project: "scribe"})
	if err != nil {
		t.Fatal(err)
	}
	if report.TotalOverlaps != 1 {
		t.Errorf("expected 1 overlap for project scribe, got %d", report.TotalOverlaps)
	}
	if report.Overlaps[0].Label != "scribe:mcp/server.go" {
		t.Errorf("expected scribe label overlap, got %s", report.Overlaps[0].Label)
	}
}

func TestComponentLabelGate_Blocks(t *testing.T) {
	os.Setenv("SCRIBE_GATE_REQUIRE_COMPONENT_LABELS", "true")
	defer os.Unsetenv("SCRIBE_GATE_REQUIRE_COMPONENT_LABELS")

	p, s := newProto(t)
	ctx := context.Background()

	_ = s.Put(ctx, &model.Artifact{
		ID: "TASK-1", Kind: "task", Status: "draft", Title: "Gated",
		Sections: []model.Section{{Name: "specification", Text: "some spec"}},
	})

	results, err := p.SetField(ctx, []string{"TASK-1"}, "status", "active")
	if err != nil {
		t.Fatal(err)
	}
	if results[0].OK {
		t.Error("expected gate to block activation, but it succeeded")
	}
	if results[0].Error == "" {
		t.Error("expected error message from gate")
	}
}

func TestComponentLabelGate_Passes(t *testing.T) {
	os.Setenv("SCRIBE_GATE_REQUIRE_COMPONENT_LABELS", "true")
	defer os.Unsetenv("SCRIBE_GATE_REQUIRE_COMPONENT_LABELS")

	p, s := newProto(t)
	ctx := context.Background()

	_ = s.Put(ctx, &model.Artifact{
		ID: "TASK-1", Kind: "task", Status: "draft", Title: "Labeled",
		Labels:   []string{"locus:internal/arch"},
		Sections: []model.Section{{Name: "specification", Text: "some spec"}},
	})

	results, err := p.SetField(ctx, []string{"TASK-1"}, "status", "active")
	if err != nil {
		t.Fatal(err)
	}
	if !results[0].OK {
		t.Errorf("expected gate to pass, got error: %s", results[0].Error)
	}
}

func TestComponentLabelGate_NoTriggerSection(t *testing.T) {
	os.Setenv("SCRIBE_GATE_REQUIRE_COMPONENT_LABELS", "true")
	defer os.Unsetenv("SCRIBE_GATE_REQUIRE_COMPONENT_LABELS")

	p, s := newProto(t)
	ctx := context.Background()

	_ = s.Put(ctx, &model.Artifact{
		ID: "TASK-1", Kind: "task", Status: "draft", Title: "No trigger",
		Sections: []model.Section{{Name: "notes", Text: "just notes"}},
	})

	results, err := p.SetField(ctx, []string{"TASK-1"}, "status", "active")
	if err != nil {
		t.Fatal(err)
	}
	if !results[0].OK {
		t.Errorf("gate should not fire for non-trigger sections, got error: %s", results[0].Error)
	}
}

func TestComponentLabelGate_DisabledByDefault(t *testing.T) {
	os.Unsetenv("SCRIBE_GATE_REQUIRE_COMPONENT_LABELS")

	p, s := newProto(t)
	ctx := context.Background()

	_ = s.Put(ctx, &model.Artifact{
		ID: "TASK-1", Kind: "task", Status: "draft", Title: "Ungated",
		Sections: []model.Section{{Name: "specification", Text: "spec"}},
	})

	results, err := p.SetField(ctx, []string{"TASK-1"}, "status", "active")
	if err != nil {
		t.Fatal(err)
	}
	if !results[0].OK {
		t.Errorf("gate should not fire when env var is unset, got error: %s", results[0].Error)
	}
}

func TestComponentLabelGate_NonTask(t *testing.T) {
	os.Setenv("SCRIBE_GATE_REQUIRE_COMPONENT_LABELS", "true")
	defer os.Unsetenv("SCRIBE_GATE_REQUIRE_COMPONENT_LABELS")

	p, s := newProto(t)
	ctx := context.Background()

	_ = s.Put(ctx, &model.Artifact{
		ID: "SPR-1", Kind: "sprint", Status: "draft", Title: "Sprint",
		Sections: []model.Section{{Name: "specification", Text: "spec"}},
	})

	results, err := p.SetField(ctx, []string{"SPR-1"}, "status", "active")
	if err != nil {
		t.Fatal(err)
	}
	if !results[0].OK {
		t.Errorf("gate should only apply to tasks, got error: %s", results[0].Error)
	}
}

// --- Cycle detection tests (FEA-2026-001) ---

func TestLinkDependsOn_SimpleChainAllowed(t *testing.T) {
	p, s := newProto(t)
	ctx := context.Background()

	_ = s.Put(ctx, &model.Artifact{ID: "A", Kind: "task", Status: "draft", Title: "A"})
	_ = s.Put(ctx, &model.Artifact{ID: "B", Kind: "task", Status: "draft", Title: "B"})

	results, err := p.LinkArtifacts(ctx, "A", "depends_on", []string{"B"})
	if err != nil {
		t.Fatalf("simple chain should succeed: %v", err)
	}
	if !results[0].OK {
		t.Errorf("expected OK, got error: %s", results[0].Error)
	}
}

func TestLinkDependsOn_DirectCycleRejected(t *testing.T) {
	p, s := newProto(t)
	ctx := context.Background()

	_ = s.Put(ctx, &model.Artifact{ID: "A", Kind: "task", Status: "draft", Title: "A"})
	_ = s.Put(ctx, &model.Artifact{ID: "B", Kind: "task", Status: "draft", Title: "B"})

	_, _ = p.LinkArtifacts(ctx, "A", "depends_on", []string{"B"})

	_, err := p.LinkArtifacts(ctx, "B", "depends_on", []string{"A"})
	if err == nil {
		t.Fatal("expected cycle error, got nil")
	}
	if !strings.Contains(err.Error(), "cycle detected") {
		t.Errorf("expected cycle error, got: %s", err.Error())
	}
	if !strings.Contains(err.Error(), "→") {
		t.Errorf("expected path in error, got: %s", err.Error())
	}
}

func TestLinkDependsOn_TransitiveCycleRejected(t *testing.T) {
	p, s := newProto(t)
	ctx := context.Background()

	_ = s.Put(ctx, &model.Artifact{ID: "A", Kind: "task", Status: "draft", Title: "A"})
	_ = s.Put(ctx, &model.Artifact{ID: "B", Kind: "task", Status: "draft", Title: "B"})
	_ = s.Put(ctx, &model.Artifact{ID: "C", Kind: "task", Status: "draft", Title: "C"})

	_, _ = p.LinkArtifacts(ctx, "A", "depends_on", []string{"B"})
	_, _ = p.LinkArtifacts(ctx, "B", "depends_on", []string{"C"})

	_, err := p.LinkArtifacts(ctx, "C", "depends_on", []string{"A"})
	if err == nil {
		t.Fatal("expected transitive cycle error, got nil")
	}
	if !strings.Contains(err.Error(), "cycle detected") {
		t.Errorf("expected cycle error, got: %s", err.Error())
	}
}

func TestLinkDependsOn_SelfLoopRejected(t *testing.T) {
	p, s := newProto(t)
	ctx := context.Background()

	_ = s.Put(ctx, &model.Artifact{ID: "A", Kind: "task", Status: "draft", Title: "A"})

	_, err := p.LinkArtifacts(ctx, "A", "depends_on", []string{"A"})
	if err == nil {
		t.Fatal("expected self-loop error, got nil")
	}
	if !strings.Contains(err.Error(), "cycle detected") {
		t.Errorf("expected cycle error, got: %s", err.Error())
	}
}

func TestLinkDependsOn_BatchWithOneBadTargetRejectsAll(t *testing.T) {
	p, s := newProto(t)
	ctx := context.Background()

	_ = s.Put(ctx, &model.Artifact{ID: "A", Kind: "task", Status: "draft", Title: "A"})
	_ = s.Put(ctx, &model.Artifact{ID: "B", Kind: "task", Status: "draft", Title: "B"})
	_ = s.Put(ctx, &model.Artifact{ID: "C", Kind: "task", Status: "draft", Title: "C"})

	_, _ = p.LinkArtifacts(ctx, "A", "depends_on", []string{"B"})

	_, err := p.LinkArtifacts(ctx, "B", "depends_on", []string{"C", "A"})
	if err == nil {
		t.Fatal("expected batch rejection, got nil")
	}

	edges, _ := s.Neighbors(ctx, "B", "depends_on", store.Outgoing)
	for _, e := range edges {
		if e.To == "C" {
			t.Error("B->C should not have been created (batch is all-or-nothing)")
		}
	}
}

func TestLinkOtherRelations_AllowCycles(t *testing.T) {
	p, s := newProto(t)
	ctx := context.Background()

	_ = s.Put(ctx, &model.Artifact{ID: "A", Kind: "task", Status: "draft", Title: "A"})
	_ = s.Put(ctx, &model.Artifact{ID: "B", Kind: "task", Status: "draft", Title: "B"})

	_, err := p.LinkArtifacts(ctx, "A", "documents", []string{"B"})
	if err != nil {
		t.Fatalf("A documents B should succeed: %v", err)
	}

	_, err = p.LinkArtifacts(ctx, "B", "documents", []string{"A"})
	if err != nil {
		t.Fatalf("B documents A should succeed (no DAG constraint): %v", err)
	}
}

func TestArtifactTree_ScopeInTreeNode(t *testing.T) {
	p, s := newProto(t)
	ctx := context.Background()

	_ = s.Put(ctx, &model.Artifact{ID: "SPR-1", Kind: "sprint", Scope: "origami", Status: "active", Title: "Sprint"})
	_ = s.Put(ctx, &model.Artifact{ID: "TASK-1", Kind: "task", Scope: "scribe", Status: "draft", Title: "Child", Parent: "SPR-1"})

	tree, err := p.ArtifactTree(ctx, protocol.TreeInput{ID: "SPR-1"})
	if err != nil {
		t.Fatal(err)
	}
	if tree.Scope != "origami" {
		t.Errorf("expected root scope 'origami', got %q", tree.Scope)
	}
	if len(tree.Children) == 0 {
		t.Fatal("expected children")
	}
	if tree.Children[0].Scope != "scribe" {
		t.Errorf("expected child scope 'scribe', got %q", tree.Children[0].Scope)
	}
}

func TestSetFieldDependsOn_CycleRejected(t *testing.T) {
	p, s := newProto(t)
	ctx := context.Background()

	_ = s.Put(ctx, &model.Artifact{ID: "A", Kind: "task", Status: "draft", Title: "A", DependsOn: []string{"B"}})
	_ = s.Put(ctx, &model.Artifact{ID: "B", Kind: "task", Status: "draft", Title: "B"})

	results, err := p.SetField(ctx, []string{"B"}, "depends_on", "A")
	if err != nil {
		t.Fatal(err)
	}
	if results[0].OK {
		t.Error("expected set_field to reject depends_on cycle")
	}
	if !strings.Contains(results[0].Error, "cycle detected") {
		t.Errorf("expected cycle error, got: %s", results[0].Error)
	}
}

// --- Vocabulary tests ---

func newProtoWithVocab(t *testing.T, vocab []string) (*protocol.Protocol, store.Store) {
	t.Helper()
	s := openStore(t)
	return protocol.New(s, nil, nil, vocab), s
}

func TestValidateKind_RejectsUnknown(t *testing.T) {
	err := model.ValidateKind("foo", []string{"goal", "sprint", "task", "spec", "bug"})
	if err == nil {
		t.Fatal("expected error for unknown kind")
	}
	if !strings.Contains(err.Error(), `unknown kind "foo"`) {
		t.Errorf("unexpected error: %s", err.Error())
	}
	if !strings.Contains(err.Error(), "scribe vocab add") {
		t.Errorf("expected hint about vocab add, got: %s", err.Error())
	}
}

func TestValidateKind_AcceptsKnown(t *testing.T) {
	err := model.ValidateKind("task", []string{"goal", "sprint", "task", "spec", "bug"})
	if err != nil {
		t.Fatalf("expected nil, got: %v", err)
	}
}

func TestValidateKind_NoVocabAcceptsAll(t *testing.T) {
	err := model.ValidateKind("anything", nil)
	if err != nil {
		t.Fatalf("nil vocab should accept all, got: %v", err)
	}
	err = model.ValidateKind("anything", []string{})
	if err != nil {
		t.Fatalf("empty vocab should accept all, got: %v", err)
	}
}

func TestCreateArtifact_EnforcesVocab(t *testing.T) {
	p, _ := newProtoWithVocab(t, []string{"task", "spec", "bug", "goal", "sprint"})
	ctx := context.Background()

	_, err := p.CreateArtifact(ctx, protocol.CreateInput{Kind: "foo", Title: "test"})
	if err == nil {
		t.Fatal("expected vocab rejection")
	}
	if !strings.Contains(err.Error(), "unknown kind") {
		t.Errorf("expected vocab error, got: %s", err.Error())
	}

	art, err := p.CreateArtifact(ctx, protocol.CreateInput{Kind: "task", Title: "test"})
	if err != nil {
		t.Fatalf("task should be accepted: %v", err)
	}
	if art.Kind != "task" {
		t.Errorf("expected kind=task, got %s", art.Kind)
	}
}

func TestSetFieldKind_EnforcesVocab(t *testing.T) {
	p, s := newProtoWithVocab(t, []string{"task", "spec", "bug", "goal", "sprint"})
	ctx := context.Background()

	_ = s.Put(ctx, &model.Artifact{ID: "TASK-1", Kind: "task", Status: "draft", Title: "A"})

	results, err := p.SetField(ctx, []string{"TASK-1"}, "kind", "foo")
	if err != nil {
		t.Fatal(err)
	}
	if results[0].OK {
		t.Error("expected vocab rejection for set_field kind=foo")
	}

	results, err = p.SetField(ctx, []string{"TASK-1"}, "kind", "spec")
	if err != nil {
		t.Fatal(err)
	}
	if !results[0].OK {
		t.Errorf("expected spec to be accepted, got: %s", results[0].Error)
	}
}

func TestVocabMigrate_DryRun(t *testing.T) {
	p, s := newProto(t)
	ctx := context.Background()

	_ = s.Put(ctx, &model.Artifact{ID: "STORY-1", Kind: "story", Status: "draft", Title: "A"})
	_ = s.Put(ctx, &model.Artifact{ID: "STORY-2", Kind: "story", Status: "draft", Title: "B"})
	_ = s.Put(ctx, &model.Artifact{ID: "EPIC-1", Kind: "epic", Status: "draft", Title: "E"})
	_ = s.Put(ctx, &model.Artifact{ID: "SPEC-1", Kind: "specification", Status: "draft", Title: "S"})
	_ = s.Put(ctx, &model.Artifact{ID: "RULE-1", Kind: "rule", Status: "draft", Title: "R"})
	_ = s.Put(ctx, &model.Artifact{ID: "TASK-1", Kind: "task", Status: "draft", Title: "T"})

	result, err := p.VocabMigrate(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	if result.Rewrites["story → task"] != 2 {
		t.Errorf("expected 2 story→task, got %d", result.Rewrites["story → task"])
	}
	if result.Rewrites["epic → goal"] != 1 {
		t.Errorf("expected 1 epic→goal, got %d", result.Rewrites["epic → goal"])
	}
	if result.Rewrites["specification → spec"] != 1 {
		t.Errorf("expected 1 specification→spec, got %d", result.Rewrites["specification → spec"])
	}
	if result.Archived != 1 {
		t.Errorf("expected 1 archived (rule), got %d", result.Archived)
	}

	art, _ := s.Get(ctx, "STORY-1")
	if art.Kind != "story" {
		t.Error("dry run should not mutate")
	}
}

func TestVocabMigrate_Commit(t *testing.T) {
	p, s := newProto(t)
	ctx := context.Background()

	_ = s.Put(ctx, &model.Artifact{ID: "STORY-1", Kind: "story", Status: "draft", Title: "A"})
	_ = s.Put(ctx, &model.Artifact{ID: "EPIC-1", Kind: "epic", Status: "draft", Title: "E"})
	_ = s.Put(ctx, &model.Artifact{ID: "RULE-1", Kind: "rule", Status: "draft", Title: "R"})

	_, err := p.VocabMigrate(ctx, false)
	if err != nil {
		t.Fatal(err)
	}

	art, _ := s.Get(ctx, "STORY-1")
	if art.Kind != "task" {
		t.Errorf("expected kind=task after migration, got %s", art.Kind)
	}
	art, _ = s.Get(ctx, "EPIC-1")
	if art.Kind != "goal" {
		t.Errorf("expected kind=goal after migration, got %s", art.Kind)
	}
	art, _ = s.Get(ctx, "RULE-1")
	if art.Status != "archived" {
		t.Errorf("expected rule to be archived, got status=%s", art.Status)
	}
}

func TestVocabAdd_AndRemove(t *testing.T) {
	p, s := newProtoWithVocab(t, []string{"task", "spec", "bug", "goal", "sprint"})
	ctx := context.Background()

	if err := p.VocabAdd("incident"); err != nil {
		t.Fatal(err)
	}
	kinds := p.VocabList()
	found := false
	for _, k := range kinds {
		if k == "incident" {
			found = true
		}
	}
	if !found {
		t.Error("incident should be in vocab after add")
	}

	if err := p.VocabRemove(ctx, "incident"); err != nil {
		t.Fatal(err)
	}

	_ = s.Put(ctx, &model.Artifact{ID: "T-1", Kind: "task", Status: "draft", Title: "X"})
	if err := p.VocabRemove(ctx, "task"); err == nil {
		t.Error("expected error removing kind with existing artifacts")
	}
}

// --- Orphan detection tests ---

func TestDetectOrphans_TaskWithoutSpec(t *testing.T) {
	p, s := newProto(t)
	ctx := context.Background()

	_ = s.Put(ctx, &model.Artifact{ID: "TASK-1", Kind: "task", Status: "draft", Title: "Lonely task"})
	_ = s.Put(ctx, &model.Artifact{ID: "SPE-1", Kind: "spec", Status: "draft", Title: "Lonely spec"})

	report, err := p.DetectOrphans(ctx, protocol.OrphanInput{})
	if err != nil {
		t.Fatal(err)
	}
	if report.TotalOrphans != 2 {
		t.Errorf("expected 2 orphans, got %d", report.TotalOrphans)
	}
	if report.TotalScanned != 2 {
		t.Errorf("expected 2 scanned, got %d", report.TotalScanned)
	}
}

func TestDetectOrphans_LinkedTaskAndSpec(t *testing.T) {
	p, s := newProto(t)
	ctx := context.Background()

	_ = s.Put(ctx, &model.Artifact{
		ID: "TASK-1", Kind: "task", Status: "draft", Title: "Linked task",
		Links: map[string][]string{"implements": {"SPE-1"}},
	})
	_ = s.Put(ctx, &model.Artifact{ID: "SPE-1", Kind: "spec", Status: "draft", Title: "Linked spec"})

	report, err := p.DetectOrphans(ctx, protocol.OrphanInput{})
	if err != nil {
		t.Fatal(err)
	}
	if report.TotalOrphans != 0 {
		t.Errorf("expected 0 orphans, got %d", report.TotalOrphans)
		for _, o := range report.Orphans {
			t.Logf("  %s: %s", o.ID, o.Reason)
		}
	}
}

func TestDetectOrphans_SkipsTerminal(t *testing.T) {
	p, s := newProto(t)
	ctx := context.Background()

	_ = s.Put(ctx, &model.Artifact{ID: "TASK-1", Kind: "task", Status: "complete", Title: "Done task"})
	_ = s.Put(ctx, &model.Artifact{ID: "SPE-1", Kind: "spec", Status: "archived", Title: "Archived spec"})

	report, err := p.DetectOrphans(ctx, protocol.OrphanInput{})
	if err != nil {
		t.Fatal(err)
	}
	if report.TotalScanned != 0 {
		t.Errorf("expected 0 scanned (terminal artifacts skipped), got %d", report.TotalScanned)
	}
}

func TestDetectOrphans_BugWithoutTask(t *testing.T) {
	p, s := newProto(t)
	ctx := context.Background()

	_ = s.Put(ctx, &model.Artifact{ID: "BUG-1", Kind: "bug", Status: "draft", Title: "Lonely bug"})

	report, err := p.DetectOrphans(ctx, protocol.OrphanInput{})
	if err != nil {
		t.Fatal(err)
	}
	if report.TotalOrphans != 1 {
		t.Errorf("expected 1 orphan, got %d", report.TotalOrphans)
	}
	if report.Orphans[0].Reason != "bug has no task implementing it" {
		t.Errorf("unexpected reason: %s", report.Orphans[0].Reason)
	}
}

func TestDetectOrphans_IgnoresGoalsAndSprints(t *testing.T) {
	p, s := newProto(t)
	ctx := context.Background()

	_ = s.Put(ctx, &model.Artifact{ID: "GOAL-1", Kind: "goal", Status: "current", Title: "A goal"})
	_ = s.Put(ctx, &model.Artifact{ID: "SPR-1", Kind: "sprint", Status: "active", Title: "A sprint"})

	report, err := p.DetectOrphans(ctx, protocol.OrphanInput{})
	if err != nil {
		t.Fatal(err)
	}
	if report.TotalScanned != 0 {
		t.Errorf("expected 0 scanned (goals/sprints not checked), got %d", report.TotalScanned)
	}
}

func TestSetGoal_CreatesSubGoal(t *testing.T) {
	p, _ := newProto(t)
	ctx := context.Background()

	res, err := p.SetGoal(ctx, protocol.SetGoalInput{Title: "North Star", Scope: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Root.Kind != "goal" {
		t.Errorf("expected root kind=goal, got %s", res.Root.Kind)
	}
}
