package protocol_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	return protocol.New(s, nil, []string{"test"}, nil, protocol.IDConfig{}), s
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
		ID: "TASK-1", Kind: "task", Status: "active", Title: "A", Scope: "test",
		Labels: []string{"locus:internal/arch"},
	})
	_ = s.Put(ctx, &model.Artifact{
		ID: "TASK-2", Kind: "task", Status: "active", Title: "B", Scope: "test",
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
		ID: "TASK-1", Kind: "task", Status: "active", Title: "Task A", Scope: "test",
		Labels: []string{"locus:internal/arch", "locus:internal/mcp"},
	})
	_ = s.Put(ctx, &model.Artifact{
		ID: "TASK-2", Kind: "task", Status: "active", Title: "Task B", Scope: "test",
		Labels: []string{"locus:internal/arch"},
	})
	_ = s.Put(ctx, &model.Artifact{
		ID: "TASK-3", Kind: "task", Status: "active", Title: "Task C", Scope: "test",
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
		ID: "TASK-1", Kind: "task", Status: "active", Title: "A", Scope: "test",
		Labels: []string{"locus:internal/arch", "scribe:mcp/server.go"},
	})
	_ = s.Put(ctx, &model.Artifact{
		ID: "TASK-2", Kind: "task", Status: "active", Title: "B", Scope: "test",
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
		ID: "TASK-1", Kind: "task", Status: "draft", Title: "Labeled", Scope: "test",
		Priority: "medium", Labels: []string{"locus:internal/arch"},
		Sections: []model.Section{
			{Name: "context", Text: "some context"},
			{Name: "checklist", Text: "items"},
			{Name: "acceptance", Text: "criteria"},
		},
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
		ID: "TASK-1", Kind: "task", Status: "draft", Title: "No trigger", Scope: "test", Priority: "medium",
		Sections: []model.Section{
			{Name: "context", Text: "context"},
			{Name: "checklist", Text: "items"},
			{Name: "acceptance", Text: "criteria"},
		},
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
		ID: "TASK-1", Kind: "task", Status: "draft", Title: "Ungated", Scope: "test", Priority: "medium",
		Sections: []model.Section{
			{Name: "context", Text: "context"},
			{Name: "checklist", Text: "items"},
			{Name: "acceptance", Text: "criteria"},
		},
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

	// Campaign has no ActivationRequiresSections; component label gate applies only to tasks.
	_ = s.Put(ctx, &model.Artifact{
		ID: "CMP-1", Kind: "campaign", Status: "draft", Title: "Campaign", Scope: "test",
		Sections: []model.Section{{Name: "mission", Text: "mission"}},
	})

	results, err := p.SetField(ctx, []string{"CMP-1"}, "status", "active")
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

	_ = s.Put(ctx, &model.Artifact{ID: "GOAL-1", Kind: "goal", Scope: "origami", Status: "current", Title: "Goal"})
	_ = s.Put(ctx, &model.Artifact{ID: "TASK-1", Kind: "task", Scope: "scribe", Status: "draft", Title: "Child", Parent: "GOAL-1"})

	tree, err := p.ArtifactTree(ctx, protocol.TreeInput{ID: "GOAL-1"})
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
	return protocol.New(s, nil, []string{"test"}, vocab, protocol.IDConfig{}), s
}

func TestValidateKind_RejectsUnknown(t *testing.T) {
	err := model.ValidateKind("foo", []string{"goal", "campaign", "task", "spec", "bug"})
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
	err := model.ValidateKind("task", []string{"goal", "campaign", "task", "spec", "bug"})
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
	p, _ := newProtoWithVocab(t, []string{"task", "spec", "bug", "goal", "campaign"})
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
	p, s := newProtoWithVocab(t, []string{"task", "spec", "bug", "goal", "campaign"})
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

func TestVocabAdd_AndRemove(t *testing.T) {
	p, s := newProtoWithVocab(t, []string{"task", "spec", "bug", "goal", "campaign"})
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

	// ref and doc kinds have RequiredOutgoing: [documents]; tasks do not.
	_ = s.Put(ctx, &model.Artifact{ID: "REF-1", Kind: "ref", Status: "draft", Title: "Lonely ref", Scope: "test"})
	_ = s.Put(ctx, &model.Artifact{ID: "DOC-1", Kind: "doc", Status: "draft", Title: "Lonely doc", Scope: "test"})

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
		ID: "TASK-1", Kind: "task", Status: "draft", Title: "Linked task", Scope: "test",
		Links: map[string][]string{"implements": {"SPE-1"}},
	})
	_ = s.Put(ctx, &model.Artifact{ID: "SPE-1", Kind: "spec", Status: "draft", Title: "Linked spec", Scope: "test"})

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

	_ = s.Put(ctx, &model.Artifact{ID: "TASK-1", Kind: "task", Status: "complete", Title: "Done task", Scope: "test"})
	_ = s.Put(ctx, &model.Artifact{ID: "SPE-1", Kind: "spec", Status: "archived", Title: "Archived spec", Scope: "test"})

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

	_ = s.Put(ctx, &model.Artifact{ID: "REF-1", Kind: "ref", Status: "draft", Title: "Lonely ref", Scope: "test"})

	report, err := p.DetectOrphans(ctx, protocol.OrphanInput{})
	if err != nil {
		t.Fatal(err)
	}
	if report.TotalOrphans != 1 {
		t.Errorf("expected 1 orphan, got %d", report.TotalOrphans)
	}
	if len(report.Orphans) > 0 && !strings.Contains(report.Orphans[0].Reason, "documents") {
		t.Errorf("unexpected reason: %s", report.Orphans[0].Reason)
	}
}

func TestDetectOrphans_IgnoresGoals(t *testing.T) {
	p, s := newProto(t)
	ctx := context.Background()

	_ = s.Put(ctx, &model.Artifact{ID: "GOAL-1", Kind: "goal", Status: "current", Title: "A goal", Scope: "test"})

	report, err := p.DetectOrphans(ctx, protocol.OrphanInput{})
	if err != nil {
		t.Fatal(err)
	}
	if report.TotalScanned != 0 {
		t.Errorf("expected 0 scanned (goals not checked), got %d", report.TotalScanned)
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

// --- Mandatory scope tests (SCR-SPC-17) ---

func TestCreateArtifact_InfersScopeFromParent(t *testing.T) {
	p, s := newProto(t)
	ctx := context.Background()

	_ = s.Put(ctx, &model.Artifact{ID: "GOAL-1", Kind: "goal", Status: "current", Title: "Goal", Scope: "parentscope"})

	art, err := p.CreateArtifact(ctx, protocol.CreateInput{
		Kind: "task", Title: "Child task", Parent: "GOAL-1",
	})
	if err != nil {
		t.Fatalf("should infer scope from parent: %v", err)
	}
	if art.Scope != "parentscope" {
		t.Errorf("expected scope=parentscope, got %s", art.Scope)
	}
}

func TestCreateArtifact_InfersScopeFromWorkspace(t *testing.T) {
	p, _ := newProto(t)
	ctx := context.Background()

	art, err := p.CreateArtifact(ctx, protocol.CreateInput{
		Kind: "task", Title: "No scope no parent",
	})
	if err != nil {
		t.Fatalf("should infer scope from single homeScope: %v", err)
	}
	if art.Scope != "test" {
		t.Errorf("expected scope=test, got %s", art.Scope)
	}
}

func TestCreateArtifact_RejectsNoScope(t *testing.T) {
	s := openStore(t)
	p := protocol.New(s, nil, []string{"a", "b"}, nil, protocol.IDConfig{})
	ctx := context.Background()

	_, err := p.CreateArtifact(ctx, protocol.CreateInput{
		Kind: "task", Title: "Ambiguous scope",
	})
	if err == nil {
		t.Fatal("expected rejection when scope is empty with multiple homeScopes")
	}
	if !strings.Contains(err.Error(), "scope is required") {
		t.Errorf("expected 'scope is required' error, got: %s", err.Error())
	}
}

func TestCreateArtifact_ExplicitScopeWins(t *testing.T) {
	p, s := newProto(t)
	ctx := context.Background()

	_ = s.Put(ctx, &model.Artifact{ID: "GOAL-1", Kind: "goal", Status: "current", Title: "Goal", Scope: "parentscope"})

	art, err := p.CreateArtifact(ctx, protocol.CreateInput{
		Kind: "task", Title: "Explicit", Scope: "explicit", Parent: "GOAL-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if art.Scope != "explicit" {
		t.Errorf("explicit scope should win over parent, got %s", art.Scope)
	}
}

func TestSetField_RejectsEmptyScope(t *testing.T) {
	p, s := newProto(t)
	ctx := context.Background()

	_ = s.Put(ctx, &model.Artifact{ID: "TASK-1", Kind: "task", Status: "draft", Title: "A", Scope: "test"})

	results, err := p.SetField(ctx, []string{"TASK-1"}, "scope", "")
	if err != nil {
		t.Fatal(err)
	}
	if results[0].OK {
		t.Error("expected set_field to reject empty scope")
	}
	if !strings.Contains(results[0].Error, "cannot be empty") {
		t.Errorf("expected 'cannot be empty' error, got: %s", results[0].Error)
	}
}

func TestSetGoal_RejectsNoScope(t *testing.T) {
	s := openStore(t)
	p := protocol.New(s, nil, []string{"a", "b"}, nil, protocol.IDConfig{})
	ctx := context.Background()

	_, err := p.SetGoal(ctx, protocol.SetGoalInput{Title: "Ambiguous"})
	if err == nil {
		t.Fatal("expected rejection when scope is empty with multiple homeScopes")
	}
	if !strings.Contains(err.Error(), "scope is required") {
		t.Errorf("expected 'scope is required' error, got: %s", err.Error())
	}
}

func TestCreateArtifact_ScopedID(t *testing.T) {
	s := openStore(t)
	schema := model.DefaultSchema()
	idc := protocol.IDConfig{
		IDFormat:  "scoped",
		ScopeKeys:  map[string]string{"testscope": "TST"},
		KindCodes:  map[string]string{"task": "TSK"},
	}
	p := protocol.New(s, schema, []string{"testscope"}, nil, idc)

	art, err := p.CreateArtifact(context.Background(), protocol.CreateInput{
		Kind:  "task",
		Title: "First scoped task",
		Scope: "testscope",
	})
	if err != nil {
		t.Fatal(err)
	}
	if art.ID != "TST-TSK-1" {
		t.Errorf("scoped ID = %q, want TST-TSK-1", art.ID)
	}

	art2, err := p.CreateArtifact(context.Background(), protocol.CreateInput{
		Kind:  "task",
		Title: "Second scoped task",
		Scope: "testscope",
	})
	if err != nil {
		t.Fatal(err)
	}
	if art2.ID != "TST-TSK-2" {
		t.Errorf("second scoped ID = %q, want TST-TSK-2", art2.ID)
	}
}

func TestCreateArtifact_LegacyID(t *testing.T) {
	s := openStore(t)
	schema := model.DefaultSchema()
	idc := protocol.IDConfig{IDFormat: "legacy"}
	p := protocol.New(s, schema, []string{"testscope"}, nil, idc)

	art, err := p.CreateArtifact(context.Background(), protocol.CreateInput{
		Kind:  "task",
		Title: "Legacy task",
		Scope: "testscope",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(art.ID, "TASK-") {
		t.Errorf("legacy ID = %q, want TASK- prefix", art.ID)
	}
}

func TestCreateArtifact_CreatedAtBackdate(t *testing.T) {
	s := openStore(t)
	schema := model.DefaultSchema()
	p := protocol.New(s, schema, []string{"test"}, nil, protocol.IDConfig{})

	art, err := p.CreateArtifact(context.Background(), protocol.CreateInput{
		Kind:      "task",
		Title:     "Backdated task",
		CreatedAt: "2026-03-01T00:00:00Z",
	})
	if err != nil {
		t.Fatal(err)
	}
	if art.CreatedAt.Month() != time.March || art.CreatedAt.Day() != 1 {
		t.Errorf("CreatedAt = %v, want 2026-03-01", art.CreatedAt)
	}
	if art.InsertedAt.IsZero() {
		t.Error("InsertedAt should be set")
	}
}

func TestSetField_InsertedAtImmutable(t *testing.T) {
	s := openStore(t)
	schema := model.DefaultSchema()
	p := protocol.New(s, schema, []string{"test"}, nil, protocol.IDConfig{})

	art, _ := p.CreateArtifact(context.Background(), protocol.CreateInput{
		Kind:  "task",
		Title: "Test immutable",
	})

	results, err := p.SetField(context.Background(), []string{art.ID}, "inserted_at", "2020-01-01T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if results[0].OK {
		t.Error("set_field on inserted_at should fail")
	}
}

func TestSetField_CreatedAtMutable(t *testing.T) {
	s := openStore(t)
	schema := model.DefaultSchema()
	idc := protocol.IDConfig{MutableCreatedAt: true}
	p := protocol.New(s, schema, []string{"test"}, nil, idc)

	art, _ := p.CreateArtifact(context.Background(), protocol.CreateInput{
		Kind:  "task",
		Title: "Test mutable created_at",
	})

	results, err := p.SetField(context.Background(), []string{art.ID}, "created_at", "2026-01-15T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if !results[0].OK {
		t.Errorf("set_field on created_at should succeed when mutable: %s", results[0].Error)
	}
}

func TestSetField_CreatedAtNotMutable(t *testing.T) {
	s := openStore(t)
	schema := model.DefaultSchema()
	idc := protocol.IDConfig{MutableCreatedAt: false}
	p := protocol.New(s, schema, []string{"test"}, nil, idc)

	art, _ := p.CreateArtifact(context.Background(), protocol.CreateInput{
		Kind:  "task",
		Title: "Test immutable created_at",
	})

	results, err := p.SetField(context.Background(), []string{art.ID}, "created_at", "2020-01-01T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if results[0].OK {
		t.Error("set_field on created_at should fail when not mutable")
	}
}

// --- Campaign tests ---

func TestCampaign_CreateWithoutScope(t *testing.T) {
	s := openStore(t)
	p := protocol.New(s, nil, []string{"test"}, nil, protocol.IDConfig{})

	// ScopeOptional removed: campaign infers scope from single homeScope like other kinds.
	art, err := p.CreateArtifact(context.Background(), protocol.CreateInput{
		Kind:  "campaign",
		Title: "Q2 DX Polish",
	})
	if err != nil {
		t.Fatalf("expected campaign creation without scope to succeed: %v", err)
	}
	if art.Scope != "test" {
		t.Errorf("campaign scope should be inferred as test, got %q", art.Scope)
	}
	if !strings.HasPrefix(art.ID, "CMP-") {
		t.Errorf("campaign ID should start with CMP-, got %q", art.ID)
	}
}

func TestCampaign_ScopedIDFallback(t *testing.T) {
	s := openStore(t)
	schema := model.DefaultSchema()
	idc := protocol.IDConfig{IDFormat: "scoped"}
	p := protocol.New(s, schema, []string{"test"}, nil, idc)

	// ScopeOptional removed: campaign gets scope from single homeScope.
	art, err := p.CreateArtifact(context.Background(), protocol.CreateInput{
		Kind:  "campaign",
		Title: "Campaign with scoped format",
	})
	if err != nil {
		t.Fatalf("campaign with scoped ID format should succeed: %v", err)
	}
	// With scope "test", scoped format yields TST-CMP-1 (or similar).
	if art.Scope != "test" {
		t.Errorf("campaign scope should be test, got %q", art.Scope)
	}
	if art.ID == "" {
		t.Error("campaign ID should be set")
	}
}

func TestCampaign_InMotd(t *testing.T) {
	s := openStore(t)
	p := protocol.New(s, nil, []string{"test"}, nil, protocol.IDConfig{})

	_, err := p.CreateArtifact(context.Background(), protocol.CreateInput{
		Kind:   "campaign",
		Title:  "Active Campaign",
		Status: "active",
	})
	if err != nil {
		t.Fatal(err)
	}

	m, err := p.Motd(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Campaigns) != 1 {
		t.Fatalf("expected 1 campaign in motd, got %d", len(m.Campaigns))
	}
	if m.Campaigns[0].Title != "Active Campaign" {
		t.Errorf("expected campaign title 'Active Campaign', got %q", m.Campaigns[0].Title)
	}
}

func TestCampaign_SetFieldScopeEmpty(t *testing.T) {
	s := openStore(t)
	p := protocol.New(s, nil, []string{"test"}, nil, protocol.IDConfig{})

	art, _ := p.CreateArtifact(context.Background(), protocol.CreateInput{
		Kind:  "campaign",
		Title: "Test scope empty",
	})

	// ScopeOptional removed: empty scope is rejected for all kinds.
	results, err := p.SetField(context.Background(), []string{art.ID}, "scope", "")
	if err != nil {
		t.Fatal(err)
	}
	if results[0].OK {
		t.Error("setting empty scope on campaign should fail")
	}
	if !strings.Contains(results[0].Error, "cannot be empty") {
		t.Errorf("expected 'cannot be empty' error, got: %s", results[0].Error)
	}
}

func TestTask_SetFieldScopeEmptyFails(t *testing.T) {
	s := openStore(t)
	p := protocol.New(s, nil, []string{"test"}, nil, protocol.IDConfig{})

	art, _ := p.CreateArtifact(context.Background(), protocol.CreateInput{
		Kind:  "task",
		Title: "Test scope required",
	})

	results, err := p.SetField(context.Background(), []string{art.ID}, "scope", "")
	if err != nil {
		t.Fatal(err)
	}
	if results[0].OK {
		t.Error("setting empty scope on task should fail")
	}
}

// --- Schema helper tests ---

func TestSchema_IsTerminal(t *testing.T) {
	s := model.DefaultSchema()
	terminal := []string{"complete", "cancelled", "dismissed", "retired", "archived"}
	for _, st := range terminal {
		if !s.IsTerminal(st) {
			t.Errorf("expected %q to be terminal", st)
		}
	}
	nonTerminal := []string{"draft", "active", "current", "open"}
	for _, st := range nonTerminal {
		if s.IsTerminal(st) {
			t.Errorf("expected %q to NOT be terminal", st)
		}
	}
}

func TestSchema_IsReadonly(t *testing.T) {
	s := model.DefaultSchema()
	if !s.IsReadonly("archived") {
		t.Error("archived should be readonly")
	}
	if s.IsReadonly("active") {
		t.Error("active should not be readonly")
	}
}

func TestSchema_SectionTiers(t *testing.T) {
	s := model.DefaultSchema()
	taskShould := s.GetShouldSections("task")
	if len(taskShould) != 2 || taskShould[0] != "checklist" || taskShould[1] != "acceptance" {
		t.Errorf("expected task should=[checklist, acceptance], got %v", taskShould)
	}
	specMust := s.GetMustSections("spec")
	if len(specMust) != 1 || specMust[0] != "problem" {
		t.Errorf("expected spec must=[problem], got %v", specMust)
	}
}

func TestSchema_MissingShouldSections(t *testing.T) {
	s := model.DefaultSchema()

	have := []model.Section{{Name: "context", Text: "..."}}
	missing := s.MissingShouldSections("task", have)
	if len(missing) != 2 {
		t.Errorf("expected 2 missing should-sections, got %v", missing)
	}

	full := []model.Section{
		{Name: "context", Text: "..."},
		{Name: "checklist", Text: "..."},
		{Name: "acceptance", Text: "..."},
	}
	missing = s.MissingShouldSections("task", full)
	if len(missing) != 0 {
		t.Errorf("expected 0 missing should-sections, got %v", missing)
	}
}

func TestSchema_DefaultStatus(t *testing.T) {
	s := model.DefaultSchema()
	if got := s.DefaultStatus("task"); got != "draft" {
		t.Errorf("expected default status 'draft', got %q", got)
	}
	if got := s.DefaultStatus("unknown_kind"); got != "draft" {
		t.Errorf("expected fallback 'draft', got %q", got)
	}
}

func TestSchema_ExpectedSections(t *testing.T) {
	s := model.DefaultSchema()
	taskSec := s.GetExpectedSections("task")
	if len(taskSec) != 3 {
		t.Fatalf("expected 3 sections for task, got %d", len(taskSec))
	}
	if taskSec[0] != "context" || taskSec[1] != "checklist" || taskSec[2] != "acceptance" {
		t.Errorf("unexpected task sections: %v", taskSec)
	}

	goalSec := s.GetExpectedSections("goal")
	if goalSec != nil {
		t.Errorf("expected nil sections for goal, got %v", goalSec)
	}
}

func TestSchema_MissingSections(t *testing.T) {
	s := model.DefaultSchema()

	have := []model.Section{
		{Name: "context", Text: "..."},
		{Name: "checklist", Text: "..."},
	}
	missing := s.MissingSections("task", have)
	if len(missing) != 1 || missing[0] != "acceptance" {
		t.Errorf("expected [acceptance], got %v", missing)
	}

	allPresent := append(have, model.Section{Name: "acceptance", Text: "..."})
	missing = s.MissingSections("task", allPresent)
	if len(missing) != 0 {
		t.Errorf("expected no missing sections, got %v", missing)
	}

	missing = s.MissingSections("goal", nil)
	if len(missing) != 0 {
		t.Errorf("expected no missing sections for goal, got %v", missing)
	}
}

func TestSchema_ValidRelation(t *testing.T) {
	s := model.DefaultSchema()
	if !s.ValidRelation("parent_of") {
		t.Error("parent_of should be valid")
	}
	if !s.ValidRelation("*") {
		t.Error("wildcard should be valid")
	}
	if s.ValidRelation("bogus") {
		t.Error("bogus should not be valid")
	}
}

// --- Activation guard tests ---

func TestActivationGuard_BlocksMissingSections(t *testing.T) {
	s := openStore(t)
	schema := model.DefaultSchema()
	p := protocol.New(s, schema, []string{"test"}, nil, protocol.IDConfig{})

	art, _ := p.CreateArtifact(context.Background(), protocol.CreateInput{
		Kind:  "task",
		Title: "Test guard",
	})

	results, err := p.SetField(context.Background(), []string{art.ID}, "status", "active")
	if err != nil {
		t.Fatal(err)
	}
	if results[0].OK {
		t.Error("activation should fail when expected sections are missing")
	}
	if !strings.Contains(results[0].Error, "missing") {
		t.Errorf("error should mention missing sections, got: %s", results[0].Error)
	}
}

func TestActivationGuard_AllowsWhenDisabled(t *testing.T) {
	s := openStore(t)
	schema := model.DefaultSchema()
	schema.Kinds["task"] = model.KindDef{Prefix: "TASK", Code: "TSK"}
	p := protocol.New(s, schema, []string{"test"}, nil, protocol.IDConfig{})

	art, _ := p.CreateArtifact(context.Background(), protocol.CreateInput{
		Kind:  "task",
		Title: "Test guard disabled",
	})

	results, err := p.SetField(context.Background(), []string{art.ID}, "status", "active")
	if err != nil {
		t.Fatal(err)
	}
	if !results[0].OK {
		t.Errorf("activation should succeed when guard is disabled, got error: %s", results[0].Error)
	}
}

func TestActivationGuard_PassesWithSections(t *testing.T) {
	s := openStore(t)
	schema := model.DefaultSchema()
	p := protocol.New(s, schema, []string{"test"}, nil, protocol.IDConfig{})

	art, _ := p.CreateArtifact(context.Background(), protocol.CreateInput{
		Kind:     "task",
		Title:    "Test guard with sections",
		Priority: "medium",
	})

	for _, sec := range []string{"context", "checklist", "acceptance"} {
		p.AttachSection(context.Background(), art.ID, sec, "content")
	}

	results, err := p.SetField(context.Background(), []string{art.ID}, "status", "active")
	if err != nil {
		t.Fatal(err)
	}
	if !results[0].OK {
		t.Errorf("activation should succeed with all sections, got error: %s", results[0].Error)
	}
}

// --- DB conformance checker tests ---

func TestCheck_CleanDB(t *testing.T) {
	s := openStore(t)
	schema := model.DefaultSchema()
	p := protocol.New(s, schema, []string{"test"}, nil, protocol.IDConfig{})

	p.CreateArtifact(context.Background(), protocol.CreateInput{Kind: "goal", Title: "G1"})

	report, err := p.Check(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if report.TotalViolations != 0 {
		t.Errorf("clean DB should have 0 violations, got %d", report.TotalViolations)
		for _, v := range report.Violations {
			t.Logf("  %s %s %s", v.ID, v.Category, v.Detail)
		}
	}
}

func TestCheck_UnknownKind(t *testing.T) {
	s := openStore(t)
	schema := model.DefaultSchema()
	p := protocol.New(s, schema, []string{"test"}, nil, protocol.IDConfig{})

	s.Put(context.Background(), &model.Artifact{ID: "X-1", Kind: "bogus", Status: "draft", Title: "Bad", Scope: "test"})

	report, err := p.Check(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if report.TotalViolations != 1 {
		t.Errorf("expected 1 violation, got %d", report.TotalViolations)
	}
}

func TestCheck_InvalidParent(t *testing.T) {
	s := openStore(t)
	schema := model.DefaultSchema()
	p := protocol.New(s, schema, []string{"test"}, nil, protocol.IDConfig{})

	s.Put(context.Background(), &model.Artifact{ID: "TSK-1", Kind: "task", Status: "draft", Title: "T", Parent: "TSK-2", Scope: "test"})
	s.Put(context.Background(), &model.Artifact{ID: "TSK-2", Kind: "task", Status: "draft", Title: "T2", Scope: "test"})

	report, err := p.Check(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, v := range report.Violations {
		if v.Category == "invalid_parent" {
			found = true
		}
	}
	if !found {
		t.Error("expected invalid_parent violation")
	}
}

func TestCheckFix_RemovesInvalidParent(t *testing.T) {
	s := openStore(t)
	schema := model.DefaultSchema()
	p := protocol.New(s, schema, []string{"test"}, nil, protocol.IDConfig{})

	s.Put(context.Background(), &model.Artifact{ID: "TSK-1", Kind: "task", Status: "draft", Title: "T", Parent: "TSK-2", Scope: "test"})
	s.Put(context.Background(), &model.Artifact{ID: "TSK-2", Kind: "task", Status: "draft", Title: "T2", Scope: "test"})

	_, fixes, err := p.CheckFix(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(fixes) == 0 {
		t.Error("expected at least one fix")
	}
	art, _ := s.Get(context.Background(), "TSK-1")
	if art.Parent != "" {
		t.Errorf("parent should be cleared, got %q", art.Parent)
	}
}

func TestMigrate_PreservesSatisfiesEdges(t *testing.T) {
	s := openStore(t)
	schema := model.DefaultSchema()
	p := protocol.New(s, schema, []string{"test"}, nil, protocol.IDConfig{})

	s.Put(context.Background(), &model.Artifact{
		ID: "TPL-1", Kind: "template", Status: "active", Title: "T", Scope: "test",
		Sections: []model.Section{{Name: "content", Text: "tpl"}},
	})
	art := &model.Artifact{
		ID: "G-1", Kind: "goal", Status: "draft", Title: "G", Scope: "test",
		Links: map[string][]string{"satisfies": {"TPL-1"}},
	}
	s.Put(context.Background(), art)

	result, err := p.Migrate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.SatisfiesRemoved != 0 {
		t.Errorf("expected satisfies links to be preserved, got %d removed", result.SatisfiesRemoved)
	}
	got, _ := s.Get(context.Background(), "G-1")
	if _, ok := got.Links["satisfies"]; !ok {
		t.Error("satisfies link was removed but should have been preserved")
	}
}

// --- Template enforcement tests ---

func createTestTemplate(t *testing.T, s *store.SQLiteStore) {
	t.Helper()
	ctx := context.Background()
	s.Put(ctx, &model.Artifact{
		ID: "SCR-TPL-1", Kind: "template", Status: "active", Title: "Task Template", Scope: "test",
		Sections: []model.Section{
			{Name: "content", Text: "full raw template markdown"},
			{Name: "context", Text: "Background and motivation"},
			{Name: "checklist", Text: "Ordered steps for execution"},
			{Name: "acceptance", Text: "Given/When/Then criteria"},
		},
	})
}

func TestTemplate_CreateConformant(t *testing.T) {
	s := openStore(t)
	schema := model.DefaultSchema()
	p := protocol.New(s, schema, []string{"test"}, nil, protocol.IDConfig{})
	createTestTemplate(t, s)

	art, err := p.CreateArtifact(context.Background(), protocol.CreateInput{
		Kind: "task", Title: "T", Scope: "test",
		Links: map[string][]string{"satisfies": {"SCR-TPL-1"}},
		Sections: []model.Section{
			{Name: "context", Text: "bg"},
			{Name: "checklist", Text: "steps"},
			{Name: "acceptance", Text: "criteria"},
		},
	})
	if err != nil {
		t.Fatalf("expected success, got: %v", err)
	}
	if art.ID == "" {
		t.Error("expected artifact to be created")
	}
}

func TestTemplate_CreateMissingSections(t *testing.T) {
	s := openStore(t)
	schema := model.DefaultSchema()
	p := protocol.New(s, schema, []string{"test"}, nil, protocol.IDConfig{})
	createTestTemplate(t, s)

	_, err := p.CreateArtifact(context.Background(), protocol.CreateInput{
		Kind: "task", Title: "T", Scope: "test",
		Links: map[string][]string{"satisfies": {"SCR-TPL-1"}},
		Sections: []model.Section{
			{Name: "context", Text: "bg"},
		},
	})
	if err == nil {
		t.Fatal("expected error for missing template sections")
	}
	if !strings.Contains(err.Error(), "acceptance") {
		t.Errorf("error should mention missing section 'acceptance', got: %s", err.Error())
	}
	if !strings.Contains(err.Error(), "checklist") {
		t.Errorf("error should mention missing section 'checklist', got: %s", err.Error())
	}
}

func TestTemplate_CreateWithZeroSections(t *testing.T) {
	s := openStore(t)
	schema := model.DefaultSchema()
	p := protocol.New(s, schema, []string{"test"}, nil, protocol.IDConfig{})
	createTestTemplate(t, s)

	_, err := p.CreateArtifact(context.Background(), protocol.CreateInput{
		Kind:  "task",
		Title: "T",
		Scope: "test",
		Links: map[string][]string{"satisfies": {"SCR-TPL-1"}},
		// NO sections at all - this should be blocked!
	})
	if err == nil {
		t.Fatal("expected error when creating artifact with no sections but linked to template")
	}
	if !strings.Contains(err.Error(), "does not conform to template") {
		t.Errorf("error should mention template conformance, got: %s", err.Error())
	}
}

func TestTemplate_DetachBlocksTemplateSection(t *testing.T) {
	s := openStore(t)
	schema := model.DefaultSchema()
	p := protocol.New(s, schema, []string{"test"}, nil, protocol.IDConfig{})
	createTestTemplate(t, s)

	s.Put(context.Background(), &model.Artifact{
		ID: "T-1", Kind: "task", Status: "draft", Title: "T", Scope: "test",
		Links: map[string][]string{"satisfies": {"SCR-TPL-1"}},
		Sections: []model.Section{
			{Name: "context", Text: "bg"},
			{Name: "checklist", Text: "steps"},
			{Name: "acceptance", Text: "criteria"},
			{Name: "notes", Text: "extra"},
		},
	})

	_, err := p.DetachSection(context.Background(), "T-1", "context")
	if err == nil {
		t.Fatal("expected error when detaching template-required section")
	}
	if !strings.Contains(err.Error(), "required by template") {
		t.Errorf("error should mention template requirement, got: %s", err.Error())
	}
}

func TestTemplate_DetachAllowsNonTemplateSection(t *testing.T) {
	s := openStore(t)
	schema := model.DefaultSchema()
	p := protocol.New(s, schema, []string{"test"}, nil, protocol.IDConfig{})
	createTestTemplate(t, s)

	s.Put(context.Background(), &model.Artifact{
		ID: "T-1", Kind: "task", Status: "draft", Title: "T", Scope: "test",
		Links: map[string][]string{"satisfies": {"SCR-TPL-1"}},
		Sections: []model.Section{
			{Name: "context", Text: "bg"},
			{Name: "checklist", Text: "steps"},
			{Name: "acceptance", Text: "criteria"},
			{Name: "notes", Text: "extra"},
		},
	})

	removed, err := p.DetachSection(context.Background(), "T-1", "notes")
	if err != nil {
		t.Fatalf("expected success, got: %v", err)
	}
	if !removed {
		t.Error("expected section to be removed")
	}
}

func TestTemplate_CheckWarnsNonConformant(t *testing.T) {
	s := openStore(t)
	schema := model.DefaultSchema()
	p := protocol.New(s, schema, []string{"test"}, nil, protocol.IDConfig{})
	createTestTemplate(t, s)

	s.Put(context.Background(), &model.Artifact{
		ID: "T-1", Kind: "task", Status: "draft", Title: "T", Scope: "test",
		Links: map[string][]string{"satisfies": {"SCR-TPL-1"}},
		Sections: []model.Section{
			{Name: "context", Text: "bg"},
		},
	})

	report, err := p.Check(context.Background(), "test")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, v := range report.Violations {
		if v.Category == "missing_template_section" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected missing_template_section violation")
	}
}

func TestTemplate_CreateWithRealisticEightSectionTemplate(t *testing.T) {
	s := openStore(t)
	schema := model.DefaultSchema()
	p := protocol.New(s, schema, []string{"test"}, nil, protocol.IDConfig{})
	ctx := context.Background()

	// Create realistic 8-section template (like TPL-2026-002)
	s.Put(ctx, &model.Artifact{
		ID: "TPL-2026-002", Kind: "template", Status: "active", Title: "Spec Template", Scope: "test",
		Sections: []model.Section{
			{Name: "content", Text: "full raw template markdown"},
			{Name: "overview", Text: "High-level summary"},
			{Name: "context", Text: "Background and motivation"},
			{Name: "requirements", Text: "Functional requirements"},
			{Name: "design", Text: "Architecture and design"},
			{Name: "implementation", Text: "Implementation details"},
			{Name: "testing", Text: "Test plan"},
			{Name: "deployment", Text: "Deployment strategy"},
			{Name: "acceptance", Text: "Acceptance criteria"},
		},
	})

	// Test 1: Success with all sections
	art, err := p.CreateArtifact(ctx, protocol.CreateInput{
		Kind: "spec", Title: "Complete Spec", Scope: "test",
		Links: map[string][]string{"satisfies": {"TPL-2026-002"}},
		Sections: []model.Section{
			{Name: "overview", Text: "Summary"},
			{Name: "context", Text: "Background"},
			{Name: "requirements", Text: "Reqs"},
			{Name: "design", Text: "Design"},
			{Name: "implementation", Text: "Impl"},
			{Name: "testing", Text: "Tests"},
			{Name: "deployment", Text: "Deploy"},
			{Name: "acceptance", Text: "Criteria"},
		},
	})
	if err != nil {
		t.Fatalf("expected success with all 8 sections, got: %v", err)
	}
	if art.ID == "" {
		t.Error("expected artifact to be created")
	}

	// Test 2: Failure with missing sections
	_, err = p.CreateArtifact(ctx, protocol.CreateInput{
		Kind: "spec", Title: "Incomplete Spec", Scope: "test",
		Links: map[string][]string{"satisfies": {"TPL-2026-002"}},
		Sections: []model.Section{
			{Name: "overview", Text: "Summary"},
			{Name: "context", Text: "Background"},
			// Missing 6 sections
		},
	})
	if err == nil {
		t.Fatal("expected error for missing sections in realistic template")
	}
	if !strings.Contains(err.Error(), "does not conform to template") {
		t.Errorf("error should mention template conformance, got: %s", err.Error())
	}
	// Verify at least a few missing sections are mentioned
	if !strings.Contains(err.Error(), "requirements") {
		t.Errorf("error should mention missing section 'requirements', got: %s", err.Error())
	}
	if !strings.Contains(err.Error(), "design") {
		t.Errorf("error should mention missing section 'design', got: %s", err.Error())
	}
}

func TestTemplate_LinkSatisfiesBlocksMissingSections(t *testing.T) {
	s := openStore(t)
	schema := model.DefaultSchema()
	p := protocol.New(s, schema, []string{"test"}, nil, protocol.IDConfig{})
	ctx := context.Background()
	createTestTemplate(t, s)

	// Create artifact with only 1 of 3 required sections
	s.Put(ctx, &model.Artifact{
		ID: "SPEC-1", Kind: "spec", Status: "draft", Title: "Incomplete Spec", Scope: "test",
		Sections: []model.Section{
			{Name: "context", Text: "Background info"},
			// Missing checklist and acceptance
		},
	})

	// Try to link to template - should be blocked
	_, err := p.LinkArtifacts(ctx, "SPEC-1", "satisfies", []string{"SCR-TPL-1"})

	if err == nil {
		t.Fatal("expected error when linking to template with missing sections")
	}
	if !strings.Contains(err.Error(), "does not conform to template") {
		t.Errorf("error should mention template conformance, got: %s", err.Error())
	}
	if !strings.Contains(err.Error(), "checklist") {
		t.Errorf("error should mention missing section 'checklist', got: %s", err.Error())
	}
	if !strings.Contains(err.Error(), "acceptance") {
		t.Errorf("error should mention missing section 'acceptance', got: %s", err.Error())
	}
}

func TestTemplate_LinkSatisfiesAllowsConformant(t *testing.T) {
	s := openStore(t)
	schema := model.DefaultSchema()
	p := protocol.New(s, schema, []string{"test"}, nil, protocol.IDConfig{})
	ctx := context.Background()
	createTestTemplate(t, s)

	// Create artifact with all required sections
	s.Put(ctx, &model.Artifact{
		ID: "SPEC-2", Kind: "spec", Status: "draft", Title: "Complete Spec", Scope: "test",
		Sections: []model.Section{
			{Name: "context", Text: "Background info"},
			{Name: "checklist", Text: "Steps to follow"},
			{Name: "acceptance", Text: "Acceptance criteria"},
		},
	})

	// Link to template - should succeed
	results, err := p.LinkArtifacts(ctx, "SPEC-2", "satisfies", []string{"SCR-TPL-1"})

	if err != nil {
		t.Fatalf("expected success when linking to template with all sections, got: %v", err)
	}
	if len(results) != 1 || !results[0].OK {
		t.Errorf("expected successful link result, got: %+v", results)
	}

	// Verify link was added
	art, _ := s.Get(ctx, "SPEC-2")
	if len(art.Links["satisfies"]) != 1 || art.Links["satisfies"][0] != "SCR-TPL-1" {
		t.Errorf("satisfies link not added, links: %+v", art.Links)
	}
}

func TestTemplate_LinkSatisfiesAllowsNonTemplate(t *testing.T) {
	s := openStore(t)
	schema := model.DefaultSchema()
	p := protocol.New(s, schema, []string{"test"}, nil, protocol.IDConfig{})
	ctx := context.Background()

	// Create two specs (not templates)
	s.Put(ctx, &model.Artifact{
		ID: "SPEC-3", Kind: "spec", Status: "draft", Title: "Source Spec", Scope: "test",
	})
	s.Put(ctx, &model.Artifact{
		ID: "SPEC-4", Kind: "spec", Status: "draft", Title: "Target Spec", Scope: "test",
	})

	// Try to link with satisfies relation to non-template - should fail with clear error
	_, err := p.LinkArtifacts(ctx, "SPEC-3", "satisfies", []string{"SPEC-4"})

	if err == nil {
		t.Fatal("expected error when satisfies target is not a template")
	}
	if !strings.Contains(err.Error(), "not a template") {
		t.Errorf("error should mention target is not a template, got: %s", err.Error())
	}
}

func TestTemplate_LinkSatisfiesRealisticEightSection(t *testing.T) {
	s := openStore(t)
	schema := model.DefaultSchema()
	p := protocol.New(s, schema, []string{"test"}, nil, protocol.IDConfig{})
	ctx := context.Background()

	// Create realistic 8-section template
	s.Put(ctx, &model.Artifact{
		ID: "TPL-2026-003", Kind: "template", Status: "active", Title: "Spec Template", Scope: "test",
		Sections: []model.Section{
			{Name: "content", Text: "full raw template markdown"},
			{Name: "overview", Text: "High-level summary"},
			{Name: "context", Text: "Background and motivation"},
			{Name: "requirements", Text: "Functional requirements"},
			{Name: "design", Text: "Architecture and design"},
			{Name: "implementation", Text: "Implementation details"},
			{Name: "testing", Text: "Test plan"},
			{Name: "deployment", Text: "Deployment strategy"},
			{Name: "acceptance", Text: "Acceptance criteria"},
		},
	})

	// Create artifact with all 8 sections
	s.Put(ctx, &model.Artifact{
		ID: "SPEC-5", Kind: "spec", Status: "draft", Title: "Complete Spec", Scope: "test",
		Sections: []model.Section{
			{Name: "overview", Text: "Summary"},
			{Name: "context", Text: "Background"},
			{Name: "requirements", Text: "Reqs"},
			{Name: "design", Text: "Design"},
			{Name: "implementation", Text: "Impl"},
			{Name: "testing", Text: "Tests"},
			{Name: "deployment", Text: "Deploy"},
			{Name: "acceptance", Text: "Criteria"},
		},
	})

	// Link to template - should succeed
	results, err := p.LinkArtifacts(ctx, "SPEC-5", "satisfies", []string{"TPL-2026-003"})

	if err != nil {
		t.Fatalf("expected success with 8-section template, got: %v", err)
	}
	if len(results) != 1 || !results[0].OK {
		t.Errorf("expected successful link result, got: %+v", results)
	}

	// Create artifact with only 2 of 8 sections
	s.Put(ctx, &model.Artifact{
		ID: "SPEC-6", Kind: "spec", Status: "draft", Title: "Incomplete Spec", Scope: "test",
		Sections: []model.Section{
			{Name: "overview", Text: "Summary"},
			{Name: "context", Text: "Background"},
		},
	})

	// Link to template - should fail
	_, err = p.LinkArtifacts(ctx, "SPEC-6", "satisfies", []string{"TPL-2026-003"})

	if err == nil {
		t.Fatal("expected error when linking to 8-section template with only 2 sections")
	}
	if !strings.Contains(err.Error(), "does not conform to template") {
		t.Errorf("error should mention template conformance, got: %s", err.Error())
	}
	// Verify at least some missing sections are mentioned
	if !strings.Contains(err.Error(), "requirements") {
		t.Errorf("error should mention missing section 'requirements', got: %s", err.Error())
	}
}

// --- Scope labels tests ---

func scopeSetup(t *testing.T, s *store.SQLiteStore, scopes ...string) {
	t.Helper()
	ctx := context.Background()
	for _, scope := range scopes {
		key := strings.ToUpper(scope)
		if len(key) > 3 {
			key = key[:3]
		}
		s.SetScopeKey(ctx, scope, key, true)
	}
}

func TestScopeLabels_SetAndGet(t *testing.T) {
	s := openStore(t)
	schema := model.DefaultSchema()
	p := protocol.New(s, schema, []string{"test"}, nil, protocol.IDConfig{})
	ctx := context.Background()

	scopeSetup(t, s, "test")

	if err := p.SetScopeLabels(ctx, "test", []string{"go", "backend"}); err != nil {
		t.Fatal("set:", err)
	}
	labels, err := p.GetScopeLabels(ctx, "test")
	if err != nil {
		t.Fatal("get:", err)
	}
	if len(labels) != 2 || labels[0] != "go" || labels[1] != "backend" {
		t.Errorf("expected [go backend], got %v", labels)
	}
}

func TestScopeLabels_QueryExpansion_AND(t *testing.T) {
	s := openStore(t)
	schema := model.DefaultSchema()
	p := protocol.New(s, schema, []string{"myrepo"}, nil, protocol.IDConfig{})
	ctx := context.Background()

	scopeSetup(t, s, "myrepo")
	p.SetScopeLabels(ctx, "myrepo", []string{"go", "backend"})

	p.CreateArtifact(ctx, protocol.CreateInput{Kind: "task", Title: "T1", Scope: "myrepo"})

	arts, err := p.ListArtifacts(ctx, protocol.ListInput{Labels: []string{"go", "backend"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(arts) != 1 {
		t.Errorf("expected 1 artifact via AND scope expansion, got %d", len(arts))
	}
}

func TestScopeLabels_QueryExpansion_OR(t *testing.T) {
	s := openStore(t)
	schema := model.DefaultSchema()
	p := protocol.New(s, schema, []string{"myrepo"}, nil, protocol.IDConfig{})
	ctx := context.Background()

	scopeSetup(t, s, "myrepo")
	p.SetScopeLabels(ctx, "myrepo", []string{"go"})

	p.CreateArtifact(ctx, protocol.CreateInput{Kind: "task", Title: "T1", Scope: "myrepo"})

	arts, err := p.ListArtifacts(ctx, protocol.ListInput{LabelsOr: []string{"go", "python"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(arts) != 1 {
		t.Errorf("expected 1 artifact via OR scope expansion, got %d", len(arts))
	}
}

func TestScopeLabels_QueryExpansion_NOT(t *testing.T) {
	s := openStore(t)
	schema := model.DefaultSchema()
	p := protocol.New(s, schema, []string{"myrepo"}, nil, protocol.IDConfig{})
	ctx := context.Background()

	scopeSetup(t, s, "myrepo")
	p.SetScopeLabels(ctx, "myrepo", []string{"go"})

	p.CreateArtifact(ctx, protocol.CreateInput{Kind: "task", Title: "T1", Scope: "myrepo"})

	arts, err := p.ListArtifacts(ctx, protocol.ListInput{ExcludeLabels: []string{"go"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(arts) != 0 {
		t.Errorf("expected 0 artifacts (excluded by scope label), got %d", len(arts))
	}
}

func TestScopeLabels_ScopeFilterPrecedence(t *testing.T) {
	s := openStore(t)
	schema := model.DefaultSchema()
	p := protocol.New(s, schema, []string{"repo1", "repo2"}, nil, protocol.IDConfig{})
	ctx := context.Background()

	scopeSetup(t, s, "repo1", "repo2")
	p.SetScopeLabels(ctx, "repo1", []string{"go"})
	p.SetScopeLabels(ctx, "repo2", []string{"go"})

	p.CreateArtifact(ctx, protocol.CreateInput{Kind: "task", Title: "T1", Scope: "repo1"})
	p.CreateArtifact(ctx, protocol.CreateInput{Kind: "task", Title: "T2", Scope: "repo2"})

	arts, err := p.ListArtifacts(ctx, protocol.ListInput{Scope: "repo1", Labels: []string{"go"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(arts) != 1 {
		t.Errorf("expected 1 artifact (scope filter takes precedence), got %d", len(arts))
	}
}

func TestScopeLabels_DirectArtifactLabel(t *testing.T) {
	s := openStore(t)
	schema := model.DefaultSchema()
	p := protocol.New(s, schema, []string{"myrepo"}, nil, protocol.IDConfig{})
	ctx := context.Background()

	scopeSetup(t, s, "myrepo")

	art, _ := p.CreateArtifact(ctx, protocol.CreateInput{Kind: "task", Title: "T1", Scope: "myrepo", Labels: []string{"urgent"}})
	_ = art

	arts, err := p.ListArtifacts(ctx, protocol.ListInput{Labels: []string{"urgent"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(arts) != 1 {
		t.Errorf("expected 1 artifact via direct label, got %d", len(arts))
	}
}

func TestFollows_SoftWarningOnActivation(t *testing.T) {
	s := openStore(t)
	p := protocol.New(s, nil, []string{"test"}, nil, protocol.IDConfig{})
	ctx := context.Background()
	scopeSetup(t, s, "test")

	taskSections := []model.Section{
		{Name: "context", Text: "ctx"},
		{Name: "checklist", Text: "- [ ] done"},
		{Name: "acceptance", Text: "it works"},
	}

	// Create two tasks with required sections for activation
	a1, _ := p.CreateArtifact(ctx, protocol.CreateInput{Kind: "task", Title: "First", Scope: "test", Priority: "medium", Sections: taskSections})
	a2, _ := p.CreateArtifact(ctx, protocol.CreateInput{Kind: "task", Title: "Second", Scope: "test", Priority: "medium", Sections: taskSections})

	// a2 follows a1 (a2 should be done after a1)
	if _, err := p.LinkArtifacts(ctx, a2.ID, model.RelFollows, []string{a1.ID}); err != nil {
		t.Fatal(err)
	}

	// Activate a2 while a1 is still draft → should succeed with warning (force to skip section checks)
	results, _ := p.SetField(ctx, []string{a2.ID}, "status", "active", protocol.SetFieldOptions{Force: true})
	if len(results) == 0 || !results[0].OK {
		t.Fatalf("expected OK, got: %v", results)
	}
	if !strings.Contains(results[0].Error, "warning") || !strings.Contains(results[0].Error, a1.ID) {
		t.Errorf("expected follows warning mentioning %s, got: %s", a1.ID, results[0].Error)
	}

	// Complete a1, then activate a3 which follows a1 → no warning
	a3, _ := p.CreateArtifact(ctx, protocol.CreateInput{Kind: "task", Title: "Third", Scope: "test", Priority: "medium", Sections: taskSections})
	p.SetField(ctx, []string{a1.ID}, "status", "complete", protocol.SetFieldOptions{Force: true})
	if _, err := p.LinkArtifacts(ctx, a3.ID, model.RelFollows, []string{a1.ID}); err != nil {
		t.Fatal(err)
	}
	results, _ = p.SetField(ctx, []string{a3.ID}, "status", "active", protocol.SetFieldOptions{Force: true})
	if len(results) == 0 || !results[0].OK {
		t.Fatalf("expected OK, got: %v", results)
	}
	if strings.Contains(results[0].Error, "warning") {
		t.Errorf("expected no warning when followed artifact is complete, got: %s", results[0].Error)
	}
}

func TestCascadingTemplate_GlobalFallback(t *testing.T) {
	s := openStore(t)
	p := protocol.New(s, nil, []string{"test"}, nil, protocol.IDConfig{})
	ctx := context.Background()
	scopeSetup(t, s, "test")

	// Create a global (scopeless) template
	globalTpl, err := p.CreateArtifact(ctx, protocol.CreateInput{
		Kind:  "template",
		Title: "Bug Template",
		Scope: "",
		Sections: []model.Section{
			{Name: "content", Text: "template content"},
			{Name: "steps_to_reproduce", Text: "describe steps"},
			{Name: "expected_behavior", Text: "what should happen"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	p.SetField(ctx, []string{globalTpl.ID}, "status", "active", protocol.SetFieldOptions{Force: true})

	// Create a bug in scope "test" — should auto-link to global template
	bug, err := p.CreateArtifact(ctx, protocol.CreateInput{
		Kind:  "bug",
		Title: "Test Bug",
		Scope: "test",
		Sections: []model.Section{
			{Name: "steps_to_reproduce", Text: "step 1"},
			{Name: "expected_behavior", Text: "should work"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Verify auto-link
	got, _ := p.GetArtifact(ctx, bug.ID)
	if targets, ok := got.Links[model.RelSatisfies]; !ok || len(targets) == 0 {
		t.Error("expected auto-link to global template via satisfies")
	} else if targets[0] != globalTpl.ID {
		t.Errorf("expected satisfies link to %s, got %s", globalTpl.ID, targets[0])
	}
}

func TestCascadingTemplate_ScopedTakesPrecedence(t *testing.T) {
	s := openStore(t)
	p := protocol.New(s, nil, []string{"test"}, nil, protocol.IDConfig{})
	ctx := context.Background()
	scopeSetup(t, s, "test")

	// Create global (scopeless) template directly via store to bypass scope inference
	s.Put(ctx, &model.Artifact{
		ID: "TPL-GLOBAL-1", Kind: "template", Scope: "", Status: "active",
		Title: "Bug Template",
		Sections: []model.Section{
			{Name: "content", Text: "global"},
		},
	})

	// Create scoped template via protocol
	scopedTpl, err := p.CreateArtifact(ctx, protocol.CreateInput{
		Kind:  "template",
		Title: "Bug Template",
		Scope: "test",
		Sections: []model.Section{
			{Name: "content", Text: "scoped"},
			{Name: "severity", Text: "describe severity"},
		},
	})
	if err != nil {
		t.Fatal("create scoped template:", err)
	}
	p.SetField(ctx, []string{scopedTpl.ID}, "status", "active", protocol.SetFieldOptions{Force: true})

	// Create a bug — should link to scoped template, not global
	bug, err := p.CreateArtifact(ctx, protocol.CreateInput{
		Kind:  "bug",
		Title: "Test Bug",
		Scope: "test",
		Sections: []model.Section{
			{Name: "severity", Text: "high"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	got, _ := p.GetArtifact(ctx, bug.ID)
	targets := got.Links[model.RelSatisfies]
	if len(targets) == 0 {
		t.Fatal("expected auto-link to template")
	}
	if targets[0] != scopedTpl.ID {
		t.Errorf("expected scoped template %s, got %s", scopedTpl.ID, targets[0])
	}
}
