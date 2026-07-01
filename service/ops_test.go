package service_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	parchment "github.com/dpopsuev/parchment"
	"github.com/dpopsuev/scribe/service"
)

// --- SCR-TSK-386: Registry and Find ---

func TestRegistry_PopulatedOnInit(t *testing.T) {
	// Given the package is imported
	// When Registry is read
	// Then it contains at least the built-in Ops (set, list)
	if len(service.Registry) == 0 {
		t.Fatal("Registry is empty — init() did not populate it")
	}
}

func TestFind_ReturnsOpForKnownName(t *testing.T) {
	// Given "set" is a registered Op
	// When Find("set") is called
	// Then the Op is returned with the correct name
	op := service.Find("set")
	if op == nil {
		t.Fatal("Find(\"set\") returned nil")
	}
	if op.Name != "set" {
		t.Errorf("op.Name = %q, want \"set\"", op.Name)
	}
}

func TestFind_ReturnsNilForUnknownName(t *testing.T) {
	// Given "nonexistent" is not a registered Op
	// When Find("nonexistent") is called
	// Then nil is returned
	if op := service.Find("nonexistent"); op != nil {
		t.Errorf("Find(\"nonexistent\") = %+v, want nil", op)
	}
}

func TestFind_ListOpRegistered(t *testing.T) {
	// Given "list" is a registered Op
	// When Find("query") is called
	// Then the Op is returned
	if service.Find("query") == nil {
		t.Fatal("Find(\"list\") returned nil — list Op not registered")
	}
}

// --- SCR-TSK-387: opSet.Run ---

func TestOpSet_BulkArchiveViaScope(t *testing.T) {
	// Given two tasks in scope "alpha" and one in "beta"
	// When set(field=status, value=archived, scope=alpha, bypass_guards=true) is called
	// Then both alpha tasks are archived, beta is untouched
	svc := newTestService(t)
	ctx := context.Background()

	a, _ := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{Labels: []string{"kind:effort.task", parchment.LabelPrefixScope + "alpha"}, Title: "A"})
	b, _ := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{Labels: []string{"kind:effort.task", parchment.LabelPrefixScope + "alpha"}, Title: "B"})
	c, _ := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{Labels: []string{"kind:effort.task", parchment.LabelPrefixScope + "beta"}, Title: "C"})

	op := service.Find("set")
	raw, _ := json.Marshal(map[string]any{
		"field": "status", "value": "archived",
		"scope": "alpha", "bypass_guards": true,
	})
	out, err := op.Run(ctx, svc, raw)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(out, "archived") < 2 {
		t.Errorf("expected 2 archived results, got: %s", out)
	}

	artA, _ := svc.Proto.GetArtifact(ctx, a.ID)
	artB, _ := svc.Proto.GetArtifact(ctx, b.ID)
	artC, _ := svc.Proto.GetArtifact(ctx, c.ID)
	if artA.Label(parchment.LabelPrefixStatus) != "archived" {
		t.Errorf("A.status = %q, want archived", artA.Label(parchment.LabelPrefixStatus))
	}
	if artB.Label(parchment.LabelPrefixStatus) != "archived" {
		t.Errorf("B.status = %q, want archived", artB.Label(parchment.LabelPrefixStatus))
	}
	if artC.Label(parchment.LabelPrefixStatus) == "archived" {
		t.Error("C should not be archived (different scope)")
	}
}

func TestOpSet_BulkDryRunPreview(t *testing.T) {
	// Given tasks exist in scope "test"
	// When set(field=status, value=archived, scope=test, bypass_guards=true, dry_run=true) is called
	// Then output says "dry run" and tasks are untouched
	svc := newTestService(t)
	ctx := context.Background()

	art, _ := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{Labels: []string{"kind:effort.task"}, Title: "T"})

	op := service.Find("set")
	raw, _ := json.Marshal(map[string]any{
		"field": "status", "value": "archived",
		"scope": "test", "bypass_guards": true, "dry_run": true,
	})
	out, err := op.Run(ctx, svc, raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "dry") {
		t.Errorf("expected dry run indication in output, got: %s", out)
	}
	unchanged, _ := svc.Proto.GetArtifact(ctx, art.ID)
	if unchanged.Label(parchment.LabelPrefixStatus) == "archived" {
		t.Error("artifact should not be archived in dry-run mode")
	}
}

func TestOpSet_ArchiveViaStatusField(t *testing.T) {
	// Given a task exists
	// When set(field=status, value=archived, bypass_guards=true) is called
	// Then the artifact is archived without guard enforcement
	svc := newTestService(t)
	ctx := context.Background()

	art, _ := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{Labels: []string{"kind:effort.task"}, Title: "T"})

	op := service.Find("set")
	raw, _ := json.Marshal(map[string]any{
		"id": art.ID, "field": "status", "value": "archived", "bypass_guards": true,
	})
	out, err := op.Run(ctx, svc, raw)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "error") {
		t.Errorf("expected archive to succeed with bypass_guards, got: %s", out)
	}
	updated, _ := svc.Proto.GetArtifact(ctx, art.ID)
	if updated.Label(parchment.LabelPrefixStatus) != "archived" {
		t.Errorf("status = %q, want archived", updated.Label(parchment.LabelPrefixStatus))
	}
}

func TestOpSet_ActivationBlockedUntilSpecRead(t *testing.T) {
	// Given a task with required sections implements a spec that has not been read
	// When set(field=status, value=active) is called
	// Then the output contains "must read" blocking the activation
	svc := newTestService(t)
	ctx := context.Background()

	spec, _ := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{Labels: []string{"kind:intent.spec"}, Title: "S"})
	task, _ := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{
		Labels: []string{"kind:effort.task"}, Title: "T",
		Sections: []parchment.Section{
			{Name: "context", Text: "ctx"}, {Name: "checklist", Text: "ok"}, {Name: "acceptance", Text: "ok"},
		},
	})
	svc.Proto.LinkArtifacts(ctx, task.ID, "implements", []string{spec.ID}, 0) //nolint:errcheck // test setup, error irrelevant to subject under test

	op := service.Find("set")
	raw, _ := json.Marshal(map[string]any{"id": task.ID, "field": "status", "value": "work.active"})
	out, err := op.Run(ctx, svc, raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "must read") {
		t.Errorf("expected 'must read' in output, got: %s", out)
	}
}

func TestOpSet_ActivationAllowedAfterSpecRead(t *testing.T) {
	// Given a task implements a spec and the spec is in ReadLog
	// When set(field=status, value=active) is called
	// Then the transition succeeds
	svc := newTestService(t)
	ctx := context.Background()

	spec, _ := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{Labels: []string{"kind:intent.spec"}, Title: "S"})
	task, _ := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{
		Labels: []string{"kind:effort.task", "priority:medium"}, Title: "T",
		Sections: []parchment.Section{
			{Name: "context", Text: "ctx"}, {Name: "checklist", Text: "ok"}, {Name: "acceptance", Text: "ok"},
		},
	})
	svc.Proto.LinkArtifacts(ctx, task.ID, "implements", []string{spec.ID}, 0) //nolint:errcheck // test setup, error irrelevant to subject under test
	svc.ReadLog[spec.ID] = true

	op := service.Find("set")
	raw, _ := json.Marshal(map[string]any{"id": task.ID, "field": "status", "value": "work.active"})
	out, err := op.Run(ctx, svc, raw)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "error") || strings.Contains(out, "must read") {
		t.Errorf("expected activation to succeed after reading spec, got: %s", out)
	}
}

func TestOpSet_SetsSingleField(t *testing.T) {
	// Given an artifact exists with title "old"
	// When set(id=X, field=title, value=new) is called
	// Then the output contains "X.title = new"
	svc := newTestService(t)
	ctx := context.Background()

	art, err := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{Labels: []string{"kind:effort.task"}, Title: "old"})
	if err != nil {
		t.Fatal(err)
	}

	op := service.Find("set")
	raw, _ := json.Marshal(map[string]any{
		"id": art.ID, "field": "title", "value": "new",
	})
	out, err := op.Run(ctx, svc, raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, art.ID+".title = new") {
		t.Errorf("expected %q.title = new in output, got: %s", art.ID, out)
	}
}

func TestOpSet_SetsMultipleIDs(t *testing.T) {
	// Given two artifacts exist
	// When set(ids=[A,B], field=priority, value=high) is called
	// Then both IDs appear in the output
	svc := newTestService(t)
	ctx := context.Background()

	a, _ := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{Labels: []string{"kind:effort.task"}, Title: "A"})
	b, _ := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{Labels: []string{"kind:effort.task"}, Title: "B"})

	op := service.Find("set")
	raw, _ := json.Marshal(map[string]any{
		"ids": []string{a.ID, b.ID}, "field": "priority", "value": "high",
	})
	out, err := op.Run(ctx, svc, raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, a.ID) {
		t.Errorf("expected %q in output, got: %s", a.ID, out)
	}
	if !strings.Contains(out, b.ID) {
		t.Errorf("expected %q in output, got: %s", b.ID, out)
	}
}

func TestOpSet_MissingIDReturnsError(t *testing.T) {
	// Given no id or ids in input
	// When set(field=title, value=x) is called
	// Then an error is returned
	svc := newTestService(t)
	op := service.Find("set")
	raw, _ := json.Marshal(map[string]any{"field": "title", "value": "x"})
	_, err := op.Run(context.Background(), svc, raw)
	if err == nil {
		t.Fatal("expected error for missing id, got nil")
	}
}

func TestOpSet_FieldErrorPropagated(t *testing.T) {
	// Given an artifact exists
	// When set with an invalid status transition is called
	// Then the output contains "error"
	svc := newTestService(t)
	ctx := context.Background()

	art, _ := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{Labels: []string{"kind:effort.task"}, Title: "T"})
	op := service.Find("set")
	raw, _ := json.Marshal(map[string]any{
		"id": art.ID, "field": "status", "value": "retired",
	})
	out, err := op.Run(ctx, svc, raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "error") {
		t.Errorf("expected error in output for invalid transition draft→retired, got: %s", out)
	}
}

func TestCreateSections_TitleKeyAlias(t *testing.T) {
	// Bug: agents send {"title":"context","body":"..."} instead of {"name":"context","text":"..."}
	// parseSections must accept "title" as alias for "name"
	svc := newTestService(t)
	ctx := context.Background()

	op := service.Find("create")
	raw, _ := json.Marshal(map[string]any{
		"kind": "knowledge.context", "title": "title-key-test", "scope": "test",
		"sections": []map[string]string{
			{"title": "content", "body": "hello from title key"},
		},
	})
	out, err := op.Run(ctx, svc, raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Stored sections: content") {
		t.Errorf("section with title key not stored: %s", out)
	}
}

func TestOpSchema_IncludesLifecycle(t *testing.T) {
	svc := newTestService(t)
	op := service.Find("schema")
	raw, _ := json.Marshal(map[string]any{"kind": "effort.task"})
	out, err := op.Run(context.Background(), svc, raw)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"lifecycle:", "default:", "work.draft", "work.active", "work.complete"} {
		if !strings.Contains(out, want) {
			t.Errorf("schema output missing %q\nfull output:\n%s", want, out)
		}
	}
}

// --- recall (RED) ---

func TestOpList_RankedReturnsMatchingArtifacts(t *testing.T) {
	// Given a note about "authentication" exists
	// When list(ranked=true, query=authentication) is called
	// Then the note appears in results
	svc := newTestService(t)
	ctx := context.Background()

	svc.Proto.CreateArtifact(ctx, parchment.CreateInput{ //nolint:errcheck // test setup, error irrelevant to subject under test
		Labels: []string{"kind:knowledge.note"}, Title: "authentication flow",
	})

	op := service.Find("query")
	raw, _ := json.Marshal(map[string]any{"ranked": true, "query": "authentication", "scope": "test"})
	out, err := op.Run(ctx, svc, raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "authentication") {
		t.Errorf("expected 'authentication' in ranked list output, got: %s", out)
	}
}

func TestOpList_RankedEmptyQueryReturnsError(t *testing.T) {
	// Given ranked=true but no query
	// When list(ranked=true) is called
	// Then an error is returned
	svc := newTestService(t)
	op := service.Find("query")
	raw, _ := json.Marshal(map[string]any{"ranked": true, "scope": "test"})
	_, err := op.Run(context.Background(), svc, raw)
	if err == nil {
		t.Fatal("expected error for ranked list with empty query, got nil")
	}
}

func TestOpList_SemanticReturnsErrorWhenNoEmbeddings(t *testing.T) {
	// Given: no EmbedFunc configured
	// When: list(mode=semantic, query=authentication)
	// Then: clear error — semantic requires an embedding backend
	t.Parallel()
	svc := newTestService(t)
	ctx := context.Background()

	op := service.Find("query")
	raw, _ := json.Marshal(map[string]any{"mode": "semantic", "query": "authentication", "scope": "test"})
	_, err := op.Run(ctx, svc, raw)
	if err == nil {
		t.Fatal("expected error when mode=semantic and no embedding backend configured")
	}
	if !strings.Contains(err.Error(), "embedding backend") {
		t.Errorf("expected 'embedding backend' in error message, got: %v", err)
	}
}

func TestOpList_HybridFallsBackToFTS_WhenNoEmbeddings(t *testing.T) {
	// Given: no EmbedFunc configured
	// When: list(mode=hybrid, query=authentication)
	// Then: falls back to FTS ranked recall — no error, returns results
	t.Parallel()
	svc := newTestService(t)
	ctx := context.Background()

	svc.Proto.CreateArtifact(ctx, parchment.CreateInput{ //nolint:errcheck // test setup
		Labels: []string{"kind:knowledge.note"}, Title: "authentication flow",
	})

	op := service.Find("query")
	raw, _ := json.Marshal(map[string]any{"mode": "hybrid", "query": "authentication", "scope": "test"})
	out, err := op.Run(ctx, svc, raw)
	if err != nil {
		t.Fatalf("hybrid with no embeddings should fall back to FTS, got error: %v", err)
	}
	if !strings.Contains(out, "authentication") {
		t.Errorf("expected 'authentication' in hybrid output, got: %s", out)
	}
}

func TestOpList_SemanticEmptyQueryReturnsError(t *testing.T) {
	// Given: semantic=true but no query
	// When: list(semantic=true)
	// Then: error is returned
	svc := newTestService(t)
	op := service.Find("query")
	raw, _ := json.Marshal(map[string]any{"mode": "semantic", "scope": "test"})
	_, err := op.Run(context.Background(), svc, raw)
	if err == nil {
		t.Fatal("expected error for semantic list with empty query, got nil")
	}
}

func TestOpList_Semantic_WithEmbeddings_ReturnsResults(t *testing.T) {
	// Given: EmbedFunc configured, embeddings in store
	// When: list(semantic=true, query=...)
	// Then: SearchSemantic is used (not FTS)
	t.Parallel()
	vocab := []string{"authentication", "security", "jwt", "token", "clock", "ptp"}
	embedFn := parchment.SemanticEmbeddingFunc(vocab)
	store := parchment.NewMemoryStore()
	proto := parchment.New(store, nil, []string{"test"}, nil, parchment.ProtocolConfig{EmbedFunc: embedFn})
	svc := service.New(proto, nil, []string{"test"})
	ctx := context.Background()

	art, _ := proto.CreateArtifact(ctx, parchment.CreateInput{Labels: []string{"kind:knowledge.note"}, Title: "authentication jwt token"})
	_, _ = proto.CreateArtifact(ctx, parchment.CreateInput{Labels: []string{"kind:knowledge.note"}, Title: "ptp clock holdover"})
	// Librarian manually puts embeddings
	authVec, _ := embedFn(ctx, "authentication jwt token")
	_ = store.PutEmbedding(ctx, art.ID, parchment.DefaultEmbedModel, "", authVec)

	op := service.Find("query")
	raw, _ := json.Marshal(map[string]any{"mode": "semantic", "query": "security token validation", "scope": "test"})
	out, err := op.Run(ctx, svc, raw)
	if err != nil {
		t.Fatalf("semantic list with embeddings failed: %v", err)
	}
	if !strings.Contains(out, "authentication") {
		t.Errorf("expected authentication note in semantic results, got: %s", out)
	}
}

func TestOpList_DepthWithRelationAndDirection(t *testing.T) {
	// Given: campaign →[parent_of]→ goal →[parent_of]→ task
	// When: list(ranked=true, query=task, depth=2, relation=parent_of, direction=inbound)
	// Then: result includes task title and its parent chain (goal, campaign)
	t.Parallel()
	svc := newTestService(t)
	ctx := context.Background()

	campaign, _ := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{
		Labels: []string{"kind:effort.campaign"}, Title: "Q3 campaign",
		Sections: []parchment.Section{{Name: "mission", Text: "ship it"}},
	})
	goal, _ := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{Labels: []string{"kind:effort.goal"}, Title: "core goal"})
	task, _ := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{
		Labels: []string{"kind:effort.task"}, Title: "write the embedder",
		Sections: []parchment.Section{{Name: "context", Text: "embed artifacts"}},
	})
	svc.Proto.LinkArtifacts(ctx, campaign.ID, "parent_of", []string{goal.ID}, 0) //nolint:errcheck // test setup
	svc.Proto.LinkArtifacts(ctx, goal.ID, "parent_of", []string{task.ID}, 0)     //nolint:errcheck // test setup

	op := service.Find("query")
	// Use the task ID directly via list — depth traversal is the subject, not search ranking.
	raw, _ := json.Marshal(map[string]any{
		"id_prefix": task.ID,
		"depth":     2,
		"relation":  "parent_of",
		"direction": "inbound",
		"scope":     "test",
	})
	out, err := op.Run(ctx, svc, raw)
	if err != nil {
		t.Fatalf("list with depth+relation+direction: %v", err)
	}
	for _, want := range []string{"write the embedder", "core goal", "Q3 campaign"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in depth output, got:\n%s", want, out)
		}
	}
}

// --- diff (RED) ---

func TestOpReplace_SwapsEdgeTarget(t *testing.T) {
	// Given A implements B
	// When link(id=A, relation=implements, replace_from=B, target=C) is called
	// Then A implements C instead of B
	svc := newTestService(t)
	ctx := context.Background()

	a, _ := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{Labels: []string{"kind:effort.task"}, Title: "A"})
	b, _ := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{Labels: []string{"kind:intent.spec"}, Title: "B"})
	c, _ := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{Labels: []string{"kind:intent.spec"}, Title: "C"})
	svc.Proto.LinkArtifacts(ctx, a.ID, "implements", []string{b.ID}, 0) //nolint:errcheck // test setup, error irrelevant to subject under test

	op := service.Find("link")
	raw, _ := json.Marshal(map[string]any{
		"id": a.ID, "relation": "implements",
		"old_target": b.ID, "target": c.ID,
	})
	out, err := op.Run(ctx, svc, raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "replaced") {
		t.Errorf("expected 'replaced' in output, got: %s", out)
	}
}

func TestOpQuery_TopoSort_ReturnsOrderedList(t *testing.T) {
	// Given a goal with two tasks where one depends on the other
	// When query(id=goal, sort=topo) is called
	// Then output lists tasks in dependency order
	svc := newTestService(t)
	ctx := context.Background()

	goal, _ := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{Labels: []string{"kind:effort.goal"}, Title: "G"})
	a, _ := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{Labels: []string{"kind:effort.task"}, Title: "First", Parent: goal.ID})
	b, _ := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{Labels: []string{"kind:effort.task"}, Title: "Second", Parent: goal.ID, DependsOn: []string{a.ID}})

	op := service.Find("query")
	if op == nil {
		t.Fatal("query Op not registered")
	}
	raw, _ := json.Marshal(map[string]any{"id": goal.ID, "sort": "topo"})
	out, err := op.Run(ctx, svc, raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, a.ID) || !strings.Contains(out, b.ID) {
		t.Errorf("expected both tasks in topo output, got: %s", out)
	}
	posA := strings.Index(out, a.ID)
	posB := strings.Index(out, b.ID)
	if posA >= posB {
		t.Errorf("First (%s) should appear before Second (%s) in topo order", a.ID, b.ID)
	}
}

func TestOpLink_ModeRemoveUnlinks(t *testing.T) {
	// Given a link exists between A and B
	// When link(id=A, relation=implements, targets=[B], mode=remove) is called
	// Then the edge is removed
	svc := newTestService(t)
	ctx := context.Background()

	a, _ := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{Labels: []string{"kind:effort.task"}, Title: "A"})
	b, _ := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{Labels: []string{"kind:intent.spec"}, Title: "B"})
	svc.Proto.LinkArtifacts(ctx, a.ID, "implements", []string{b.ID}, 0) //nolint:errcheck // test setup, error irrelevant to subject under test

	op := service.Find("link")
	raw, _ := json.Marshal(map[string]any{"id": a.ID, "relation": "implements", "targets": []string{b.ID}, "mode": "remove"})
	out, err := op.Run(ctx, svc, raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "unlinked") {
		t.Errorf("expected 'unlinked' in output, got: %s", out)
	}
}

func TestOpLink_ModeReplaceSwapsTarget(t *testing.T) {
	// Given A implements B
	// When link(id=A, relation=implements, target=C, replace_from=B) is called
	// Then A implements C
	svc := newTestService(t)
	ctx := context.Background()

	a, _ := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{Labels: []string{"kind:effort.task"}, Title: "A"})
	b, _ := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{Labels: []string{"kind:intent.spec"}, Title: "B"})
	c, _ := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{Labels: []string{"kind:intent.spec"}, Title: "C"})
	svc.Proto.LinkArtifacts(ctx, a.ID, "implements", []string{b.ID}, 0) //nolint:errcheck // test setup, error irrelevant to subject under test

	op := service.Find("link")
	if op == nil {
		t.Fatal("link Op not registered")
	}
	raw, _ := json.Marshal(map[string]any{
		"id": a.ID, "relation": "implements",
		"target": c.ID, "old_target": b.ID,
	})
	out, err := op.Run(ctx, svc, raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "replaced") {
		t.Errorf("expected 'replaced' in output, got: %s", out)
	}
}

func TestOpLink_CreatesEdge(t *testing.T) {
	// Given two artifacts exist
	// When link(id=A, relation=implements, targets=[B]) is called
	// Then the edge exists
	svc := newTestService(t)
	ctx := context.Background()

	a, _ := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{Labels: []string{"kind:effort.task"}, Title: "A"})
	b, _ := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{Labels: []string{"kind:intent.spec"}, Title: "B"})

	op := service.Find("link")
	if op == nil {
		t.Fatal("link Op not registered")
	}
	raw, _ := json.Marshal(map[string]any{"id": a.ID, "relation": "implements", "targets": []string{b.ID}})
	out, err := op.Run(ctx, svc, raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "linked") || !strings.Contains(out, a.ID) || !strings.Contains(out, b.ID) {
		t.Errorf("expected 'linked' with IDs in output, got: %s", out)
	}
}

func TestOpUnlink_RemovesEdge(t *testing.T) {
	// Given a link exists between two artifacts
	// When link(id=A, relation=implements, targets=[B], mode=remove) is called
	// Then the edge is removed
	svc := newTestService(t)
	ctx := context.Background()

	a, _ := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{Labels: []string{"kind:effort.task"}, Title: "A"})
	b, _ := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{Labels: []string{"kind:intent.spec"}, Title: "B"})
	svc.Proto.LinkArtifacts(ctx, a.ID, "implements", []string{b.ID}, 0) //nolint:errcheck // test setup, error irrelevant to subject under test

	op := service.Find("link")
	raw, _ := json.Marshal(map[string]any{"id": a.ID, "relation": "implements", "targets": []string{b.ID}, "mode": "remove"})
	out, err := op.Run(ctx, svc, raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "unlinked") || !strings.Contains(out, a.ID) || !strings.Contains(out, b.ID) {
		t.Errorf("expected 'unlinked' with IDs in output, got: %s", out)
	}
}

func TestOpGet_Summary(t *testing.T) {
	// Given an artifact exists
	// When get(id=X, format=summary) is called
	// Then output is compact JSON with id, title, kind, status
	svc := newTestService(t)
	ctx := context.Background()

	art, _ := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{Labels: []string{"kind:knowledge.note"}, Title: "N"})

	op := service.Find("get")
	raw, _ := json.Marshal(map[string]any{"id": art.ID, "format": "summary"})
	out, err := op.Run(ctx, svc, raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, art.ID) || !strings.Contains(out, "N") {
		t.Errorf("expected summary JSON with id and title, got: %s", out)
	}
}

func TestOpGet_Briefing(t *testing.T) {
	// Given an artifact with children exists
	// When get(id=X, format=briefing) is called
	// Then output contains the tree with edge labels
	svc := newTestService(t)
	ctx := context.Background()

	parent, _ := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{Labels: []string{"kind:effort.goal"}, Title: "G"})
	child, _ := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{Labels: []string{"kind:effort.task"}, Title: "T", Parent: parent.ID})

	op := service.Find("get")
	raw, _ := json.Marshal(map[string]any{"id": parent.ID, "format": "briefing"})
	out, err := op.Run(ctx, svc, raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, child.ID) {
		t.Errorf("expected child in briefing, got: %s", out)
	}
}

func TestOpGet_Impact(t *testing.T) {
	// Given an artifact exists
	// When get(id=X, format=impact) is called
	// Then output describes impact analysis
	svc := newTestService(t)
	ctx := context.Background()

	art, _ := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{Labels: []string{"kind:effort.task"}, Title: "T"})

	op := service.Find("get")
	raw, _ := json.Marshal(map[string]any{"id": art.ID, "format": "impact"})
	out, err := op.Run(ctx, svc, raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Impact analysis") {
		t.Errorf("expected impact analysis in output, got: %s", out)
	}
}

func TestOpGet_Tree(t *testing.T) {
	// Given an artifact with a child exists
	// When get(id=X, format=tree) is called
	// Then output contains the tree structure
	svc := newTestService(t)
	ctx := context.Background()

	parent, _ := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{Labels: []string{"kind:effort.goal"}, Title: "G"})
	child, _ := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{Labels: []string{"kind:effort.task"}, Title: "T", Parent: parent.ID})

	op := service.Find("get")
	raw, _ := json.Marshal(map[string]any{"id": parent.ID, "format": "tree"})
	out, err := op.Run(ctx, svc, raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, child.ID) {
		t.Errorf("expected child in tree, got: %s", out)
	}
}

func TestOpGet_BulkIDs(t *testing.T) {
	// Given two artifacts exist
	// When get(ids=[A, B]) is called
	// Then output is a JSON array containing both
	svc := newTestService(t)
	ctx := context.Background()

	a, _ := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{Labels: []string{"kind:knowledge.note"}, Title: "A"})
	b, _ := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{Labels: []string{"kind:knowledge.note"}, Title: "B"})

	op := service.Find("get")
	raw, _ := json.Marshal(map[string]any{"ids": []string{a.ID, b.ID}})
	out, err := op.Run(ctx, svc, raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, a.ID) || !strings.Contains(out, b.ID) {
		t.Errorf("expected both IDs in bulk get output, got: %s", out)
	}
}

func TestOpCreate_BatchCreate(t *testing.T) {
	// Given a batch of artifacts to create
	// When create(artifacts=[...]) is called
	// Then all artifacts are created and appear in output
	svc := newTestService(t)
	ctx := context.Background()

	op := service.Find("create")
	raw, _ := json.Marshal(map[string]any{
		"artifacts": []map[string]any{
			{"kind": "effort.task", "title": "First", "scope": "test"},
			{"kind": "effort.task", "title": "Second", "scope": "test"},
		},
	})
	out, err := op.Run(ctx, svc, raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "First") || !strings.Contains(out, "Second") {
		t.Errorf("expected both artifacts in batch output, got: %s", out)
	}
}

func TestOpCreate_Clone(t *testing.T) {
	// Given an artifact exists
	// When create(clone_from=X, title=clone, scope=other) is called
	// Then a clone is created with the new title
	svc := newTestService(t)
	ctx := context.Background()

	source, _ := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{Labels: []string{"kind:knowledge.note"}, Title: "original"})

	op := service.Find("create")
	raw, _ := json.Marshal(map[string]any{"clone_from": source.ID, "title": "clone", "scope": "test"})
	out, err := op.Run(ctx, svc, raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, source.ID) || !strings.Contains(out, "clone") {
		t.Errorf("expected clone reference in output, got: %s", out)
	}
}

func TestOpGet_ReturnsMarkdown(t *testing.T) {
	// Given an artifact exists
	// When get(id=X) is called
	// Then output contains the artifact title in markdown format
	svc := newTestService(t)
	ctx := context.Background()

	art, _ := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{Labels: []string{"kind:knowledge.note"}, Title: "design doc"})

	op := service.Find("get")
	if op == nil {
		t.Fatal("get Op not registered")
	}
	raw, _ := json.Marshal(map[string]any{"id": art.ID})
	out, err := op.Run(ctx, svc, raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "design doc") {
		t.Errorf("expected title in output, got: %s", out)
	}
}

func TestOpGet_RecordsRead(t *testing.T) {
	// Given an artifact exists
	// When get(id=X) is called
	// Then svc.ReadLog records the ID
	svc := newTestService(t)
	ctx := context.Background()

	art, _ := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{Labels: []string{"kind:knowledge.note"}, Title: "N"})

	op := service.Find("get")
	if op == nil {
		t.Fatal("get Op not registered")
	}
	raw, _ := json.Marshal(map[string]any{"id": art.ID})
	if _, err := op.Run(ctx, svc, raw); err != nil {
		t.Fatal(err)
	}
	if !svc.ReadLog[art.ID] {
		t.Error("expected ReadLog to record the artifact ID after get")
	}
}

func TestOpCreate_ReturnsID(t *testing.T) {
	// Given a valid create input
	// When create(kind=task, title=T, scope=test) is called
	// Then output contains the artifact ID
	svc := newTestService(t)
	ctx := context.Background()

	op := service.Find("create")
	if op == nil {
		t.Fatal("create Op not registered")
	}
	raw, _ := json.Marshal(map[string]any{"kind": "effort.task", "title": "my task", "scope": "test"})
	out, err := op.Run(ctx, svc, raw)
	if err != nil {
		t.Fatal(err)
	}
	// IDs are title-derived slugs — find the second token (first is "created").
	tokens := strings.Fields(out)
	if len(tokens) < 2 || tokens[1] == "" {
		t.Errorf("expected artifact ID in output, got: %s", out)
	}
	if !strings.Contains(out, "my task") {
		t.Errorf("expected title in output, got: %s", out)
	}
}

func TestOpCreate_WithParentShowsParent(t *testing.T) {
	// Given a goal exists
	// When create(kind=task, parent=goal-id) is called
	// Then output includes "(parent: goal-id)"
	svc := newTestService(t)
	ctx := context.Background()

	goal, _ := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{Labels: []string{"kind:effort.goal"}, Title: "G"})

	op := service.Find("create")
	raw, _ := json.Marshal(map[string]any{"kind": "effort.task", "title": "child", "scope": "test", "parent": goal.ID})
	out, err := op.Run(ctx, svc, raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "parent") {
		t.Errorf("expected parent hint in output, got: %s", out)
	}
}

func TestOpCreate_MissingTitleErrors(t *testing.T) {
	// Given no title is provided
	// When create(kind=task, scope=test) is called
	// Then an error is returned
	svc := newTestService(t)
	op := service.Find("create")
	raw, _ := json.Marshal(map[string]any{"kind": "effort.task", "scope": "test"})
	_, err := op.Run(context.Background(), svc, raw)
	if err == nil {
		t.Fatal("expected error for missing title, got nil")
	}
}

func TestOpList_KindPrefixKnowledgeGrouped(t *testing.T) {
	// Given notes and concepts exist alongside a task
	// When list(kind=knowledge, group_by=kind) is called with prefix matching
	// Then knowledge artifacts appear but the task does not
	svc := newTestService(t, "test")
	ctx := context.Background()

	note, _ := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{Labels: []string{"kind:knowledge.note"}, Title: "design note"})
	concept, _ := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{Labels: []string{"kind:knowledge.concept"}, Title: "key concept"})
	task, _ := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{Labels: []string{"kind:effort.task"}, Title: "a task"})

	op := service.Find("query")
	raw, _ := json.Marshal(map[string]any{"kind": "knowledge", "group_by": "kind", "scope": "test"})
	out, err := op.Run(ctx, svc, raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, note.ID) || !strings.Contains(out, concept.ID) {
		t.Errorf("expected knowledge artifacts in output, got: %s", out[:min(300, len(out))])
	}
	if strings.Contains(out, task.ID) {
		t.Errorf("task should not appear in knowledge prefix list, got: %s", out[:min(300, len(out))])
	}
}

func TestOpUpdate_SetsMultipleFields(t *testing.T) {
	// Given a task exists
	// When update(id=X, title=new, priority=high) is called
	// Then both fields change
	svc := newTestService(t)
	ctx := context.Background()

	art, _ := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{Labels: []string{"kind:effort.task"}, Title: "old"})

	op := service.Find("update")
	if op == nil {
		t.Fatal("update Op not registered")
	}
	raw, _ := json.Marshal(map[string]any{"id": art.ID, "title": "new", "priority": "high"})
	out, err := op.Run(ctx, svc, raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "title") || !strings.Contains(out, "priority") {
		t.Errorf("expected both field updates in output, got: %s", out)
	}
	updated, _ := svc.Proto.GetArtifact(ctx, art.ID)
	if updated.Title != "new" {
		t.Errorf("title = %q, want new", updated.Title)
	}
	if updated.Label(parchment.LabelPrefixPriority) != "high" {
		t.Errorf("priority = %q, want high", updated.Label(parchment.LabelPrefixPriority))
	}
}

func TestOpUpdate_AttachesSection(t *testing.T) {
	// Given a task exists
	// When update(id=X, sections=[{name:summary, text:body}]) is called
	// Then the section is attached
	svc := newTestService(t)
	ctx := context.Background()

	art, _ := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{Labels: []string{"kind:effort.task"}, Title: "T"})

	op := service.Find("update")
	raw, _ := json.Marshal(map[string]any{
		"id":       art.ID,
		"sections": []map[string]string{{"name": "summary", "text": "the summary"}},
	})
	out, err := op.Run(ctx, svc, raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "summary") {
		t.Errorf("expected section name in output, got: %s", out)
	}
	updated, _ := svc.Proto.GetArtifact(ctx, art.ID)
	if len(updated.Sections) == 0 {
		t.Error("expected section to be attached")
	}
}

func TestOpUpdate_DeletesSection(t *testing.T) {
	// Given a task has a section "notes"
	// When update(id=X, sections_delete=["notes"]) is called
	// Then the section is removed
	svc := newTestService(t)
	ctx := context.Background()

	art, _ := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{Labels: []string{"kind:effort.task"}, Title: "T"})
	svc.Proto.AttachSection(ctx, art.ID, "notes", "content") //nolint:errcheck // test setup, error irrelevant to subject under test

	op := service.Find("update")
	raw, _ := json.Marshal(map[string]any{"id": art.ID, "sections_delete": []string{"notes"}})
	out, err := op.Run(ctx, svc, raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "removed") {
		t.Errorf("expected 'removed' in output, got: %s", out)
	}
	updated, _ := svc.Proto.GetArtifact(ctx, art.ID)
	for _, sec := range updated.Sections {
		if sec.Name == "notes" {
			t.Error("section 'notes' still present after sections_delete")
		}
	}
}

func TestOpUpdate_MissingBothFieldsAndSectionsErrors(t *testing.T) {
	// Given no fields or sections are provided
	// When update(id=X) is called
	// Then an error is returned
	svc := newTestService(t)
	ctx := context.Background()

	art, _ := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{Labels: []string{"kind:effort.task"}, Title: "T"})

	op := service.Find("update")
	raw, _ := json.Marshal(map[string]any{"id": art.ID})
	_, err := op.Run(ctx, svc, raw)
	if err == nil {
		t.Fatal("expected error for update with no changes, got nil")
	}
}

func TestOpGet_DiffAgainst(t *testing.T) {
	// Given two artifacts with different titles
	// When get(id=A, diff_against=B) is called
	// Then output contains the title difference
	svc := newTestService(t)
	ctx := context.Background()

	a, _ := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{Labels: []string{"kind:effort.task"}, Title: "Alpha"})
	b, _ := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{Labels: []string{"kind:effort.task"}, Title: "Beta"})

	op := service.Find("get")
	if op != nil {
		raw, _ := json.Marshal(map[string]any{"id": a.ID, "against": b.ID})
		out, err := op.Run(ctx, svc, raw)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "title") {
			t.Errorf("expected diff output containing 'title', got: %s", out)
		}
	}
}

// --- detach_section (RED) ---

// --- SCR-TSK-388: opList.Run ---

func TestOpList_DefaultTableOutput(t *testing.T) {
	// Given two artifacts exist
	// When list() is called with no filters
	// Then output contains both artifact IDs
	svc := newTestService(t)
	ctx := context.Background()

	a, _ := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{Labels: []string{"kind:effort.task"}, Title: "Alpha"})
	b, _ := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{Labels: []string{"kind:effort.task"}, Title: "Beta"})

	op := service.Find("query")
	raw, _ := json.Marshal(map[string]any{})
	out, err := op.Run(ctx, svc, raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, a.ID) {
		t.Errorf("expected %q in list output", a.ID)
	}
	if !strings.Contains(out, b.ID) {
		t.Errorf("expected %q in list output", b.ID)
	}
}

func TestOpList_CountMode(t *testing.T) {
	// Given 3 artifacts exist
	// When list(count=true) is called
	// Then output is the string "3"
	svc := newTestService(t)
	ctx := context.Background()

	for range 3 {
		svc.Proto.CreateArtifact(ctx, parchment.CreateInput{Labels: []string{"kind:effort.task"}, Title: "T"}) //nolint:errcheck // test setup, error irrelevant to subject under test
	}
	op := service.Find("query")
	raw, _ := json.Marshal(map[string]any{"count": true})
	out, err := op.Run(ctx, svc, raw)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out) != "3" {
		t.Errorf("count mode: got %q, want \"3\"", strings.TrimSpace(out))
	}
}

func TestOpList_TopNReturnsJSON(t *testing.T) {
	// Given 5 artifacts exist
	// When list(top=2) is called
	// Then output is valid JSON array with at most 2 elements
	svc := newTestService(t)
	ctx := context.Background()

	for range 5 {
		svc.Proto.CreateArtifact(ctx, parchment.CreateInput{Labels: []string{"kind:effort.task"}, Title: "T"}) //nolint:errcheck // test setup, error irrelevant to subject under test
	}
	op := service.Find("query")
	raw, _ := json.Marshal(map[string]any{"top": 2})
	out, err := op.Run(ctx, svc, raw)
	if err != nil {
		t.Fatal(err)
	}
	var results []any
	if err := json.Unmarshal([]byte(out), &results); err != nil {
		t.Fatalf("top=2 output not valid JSON: %v\ngot: %s", err, out)
	}
	if len(results) > 2 {
		t.Errorf("top=2: got %d results, want ≤2", len(results))
	}
}

func TestOpList_CompactFields(t *testing.T) {
	// Given an artifact exists
	// When list(fields=[id,title]) is called
	// Then output contains tab-separated ID and TITLE header
	svc := newTestService(t)
	ctx := context.Background()

	svc.Proto.CreateArtifact(ctx, parchment.CreateInput{Labels: []string{"kind:effort.task"}, Title: "Compact"}) //nolint:errcheck // test setup, error irrelevant to subject under test

	op := service.Find("query")
	raw, _ := json.Marshal(map[string]any{"fields": []string{"id", "title"}})
	out, err := op.Run(ctx, svc, raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "ID") || !strings.Contains(out, "TITLE") {
		t.Errorf("compact fields: expected ID and TITLE header, got: %s", out)
	}
}

func TestOpList_CompactInvalidFieldError(t *testing.T) {
	// Given "bogus" is not a valid field name
	// When list(fields=[bogus]) is called
	// Then an error is returned
	svc := newTestService(t)
	op := service.Find("query")
	raw, _ := json.Marshal(map[string]any{"fields": []string{"bogus"}})
	_, err := op.Run(context.Background(), svc, raw)
	if err == nil {
		t.Fatal("expected error for unknown field, got nil")
	}
}

func TestOpList_FilterByKind(t *testing.T) {
	// Given a task and a note exist
	// When list(kind=task) is called
	// Then only the task appears
	svc := newTestService(t)
	ctx := context.Background()

	task, _ := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{Labels: []string{"kind:effort.task"}, Title: "T"})
	note, _ := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{Labels: []string{"kind:knowledge.note"}, Title: "N"})

	op := service.Find("query")
	raw, _ := json.Marshal(map[string]any{"kind": "effort.task"})
	out, err := op.Run(ctx, svc, raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, task.ID) {
		t.Errorf("expected task %q in output", task.ID)
	}
	if strings.Contains(out, note.ID) {
		t.Errorf("note %q should not appear in kind=task list", note.ID)
	}
}

func TestOpList_UnfilteredHint(t *testing.T) {
	// Given artifacts exist
	// When list() is called with no filters
	// Then output contains the unfiltered hint
	svc := newTestService(t)
	ctx := context.Background()

	svc.Proto.CreateArtifact(ctx, parchment.CreateInput{Labels: []string{"kind:effort.task"}, Title: "T"}) //nolint:errcheck // test setup, error irrelevant to subject under test

	op := service.Find("query")
	raw, _ := json.Marshal(map[string]any{})
	out, err := op.Run(ctx, svc, raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "top=10") {
		t.Errorf("expected unfiltered hint containing top=10, got: %s", out)
	}
}

func TestReciprocalRankFusion(t *testing.T) {
	a := &parchment.Artifact{ID: "A", Title: "A"}
	b := &parchment.Artifact{ID: "B", Title: "B"}
	c := &parchment.Artifact{ID: "C", Title: "C"}

	semantic := []parchment.ScoredArtifact{
		{Artifact: a, Score: 0.9},
		{Artifact: b, Score: 0.7},
	}
	fts := []*parchment.Artifact{b, c}

	results := service.ReciprocalRankFusion(semantic, fts)
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
	if results[0].Artifact.ID != "B" {
		t.Errorf("B should be first (appears in both lists), got %s", results[0].Artifact.ID)
	}
	if results[0].Score <= results[1].Score {
		t.Errorf("B's score should be highest (dual-list boost)")
	}
}

func TestOpSynthesize_CreatesNoteWithCitations(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	src1, _ := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{
		Labels: []string{"kind:knowledge.source"}, Title: "Source Alpha",
	})
	src2, _ := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{
		Labels: []string{"kind:knowledge.source"}, Title: "Source Beta",
	})

	op := service.Find("synthesize")
	if op == nil {
		t.Fatal("synthesize op not registered")
	}
	raw, _ := json.Marshal(map[string]any{
		"title":   "Synthesis of Alpha and Beta",
		"body":    "Combining insights from both sources...",
		"sources": []string{src1.ID, src2.ID},
		"scope":   "test",
	})
	out, err := op.Run(ctx, svc, raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "cited 2 source(s)") {
		t.Errorf("expected citation count in output, got: %s", out)
	}
}

func TestOpSynthesize_RequiresTitleAndBody(t *testing.T) {
	svc := newTestService(t)
	op := service.Find("synthesize")
	raw, _ := json.Marshal(map[string]any{"title": "Only title"})
	_, err := op.Run(context.Background(), svc, raw)
	if err == nil {
		t.Fatal("expected error for missing body")
	}
}

func TestOpQuery_ExcerptChars(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	svc.Proto.CreateArtifact(ctx, parchment.CreateInput{
		Labels:   []string{"kind:knowledge.note"},
		Title:    "JWT Authentication",
		Sections: []parchment.Section{{Name: "body", Text: "JSON Web Tokens are used for stateless authentication in microservices architectures."}},
	})

	op := service.Find("query")
	raw, _ := json.Marshal(map[string]any{"query": "JWT", "scope": "test", "excerpt_chars": 40})
	out, err := op.Run(ctx, svc, raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "JSON Web Tokens") {
		t.Errorf("expected excerpt containing 'JSON Web Tokens', got:\n%s", out)
	}
}

func TestOpLint_FindsOrphans(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	svc.Proto.CreateArtifact(ctx, parchment.CreateInput{
		Labels: []string{"kind:knowledge.note"},
		Title:  "Orphan Note",
	})

	op := service.Find("lint")
	if op == nil {
		t.Fatal("lint op not registered")
	}
	raw, _ := json.Marshal(map[string]any{"scope": "test"})
	out, err := op.Run(ctx, svc, raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "orphan") {
		t.Errorf("expected orphan finding, got: %s", out)
	}
	if !strings.Contains(out, "Orphan Note") {
		t.Errorf("expected orphan note in output, got: %s", out)
	}
}

func TestOpLint_NoIssuesWhenClean(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	parent, _ := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{
		Labels: []string{"kind:effort.goal"},
		Title:  "Goal",
	})
	svc.Proto.CreateArtifact(ctx, parchment.CreateInput{
		Labels: []string{"kind:effort.task"},
		Title:  "Task",
		Parent: parent.ID,
	})

	op := service.Find("lint")
	raw, _ := json.Marshal(map[string]any{"scope": "test"})
	out, err := op.Run(ctx, svc, raw)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "orphan") {
		t.Errorf("linked artifacts should not be orphans, got: %s", out)
	}
}

func TestOpQuery_ExcerptCharsZeroOff(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	svc.Proto.CreateArtifact(ctx, parchment.CreateInput{
		Labels:   []string{"kind:knowledge.note"},
		Title:    "Hidden Body",
		Sections: []parchment.Section{{Name: "body", Text: "This body text should not appear in default query output."}},
	})

	op := service.Find("query")
	raw, _ := json.Marshal(map[string]any{"scope": "test"})
	out, err := op.Run(ctx, svc, raw)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "should not appear") {
		t.Errorf("excerpt_chars=0 should not include body text, got:\n%s", out)
	}
}

// --- Schema op ---

func TestOpSchema_ReturnsRelationsForKind(t *testing.T) {
	svc := newTestService(t)
	op := service.Find("schema")
	if op == nil {
		t.Fatal("schema op not registered")
	}
	raw, _ := json.Marshal(map[string]any{"kind": "knowledge.note"})
	out, err := op.Run(context.Background(), svc, raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "RELATION") {
		t.Errorf("expected table header, got: %s", out)
	}
	if !strings.Contains(out, "cites") {
		t.Errorf("expected cites relation for knowledge.note, got: %s", out)
	}
}

func TestOpSchema_InfersKindFromID(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	art, _ := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{
		Labels: []string{"kind:effort.task"}, Title: "Schema test task",
	})
	op := service.Find("schema")
	raw, _ := json.Marshal(map[string]any{"id": art.ID})
	out, err := op.Run(ctx, svc, raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "effort.task") {
		t.Errorf("expected effort.task in output, got: %s", out)
	}
}

func TestOpSchema_RequiresKindOrID(t *testing.T) {
	svc := newTestService(t)
	op := service.Find("schema")
	raw, _ := json.Marshal(map[string]any{})
	_, err := op.Run(context.Background(), svc, raw)
	if err == nil {
		t.Fatal("expected error for missing kind and id")
	}
}

// --- History op ---

func TestOpHistory_ReturnsEvents(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	art, _ := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{
		Labels: []string{"kind:knowledge.note"}, Title: "History test",
	})
	op := service.Find("history")
	raw, _ := json.Marshal(map[string]any{"id": art.ID})
	out, err := op.Run(ctx, svc, raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "created") {
		t.Errorf("expected created event, got: %s", out)
	}
}

func TestOpHistory_RequiresIDOrScope(t *testing.T) {
	svc := newTestService(t)
	op := service.Find("history")
	raw, _ := json.Marshal(map[string]any{})
	_, err := op.Run(context.Background(), svc, raw)
	if err == nil {
		t.Fatal("expected error for missing id and scope")
	}
}

// --- Hygiene op ---

func TestOpHygiene_DetectsStaleActiveTasks(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	svc.Proto.CreateArtifact(ctx, parchment.CreateInput{
		Labels: []string{"kind:effort.task"}, Title: "Fresh active",
	})
	op := service.Find("hygiene")
	raw, _ := json.Marshal(map[string]any{"scope": "test"})
	out, err := op.Run(ctx, svc, raw)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "stale_active") && strings.Contains(out, "Fresh active") {
		t.Errorf("fresh task should not be flagged stale, got: %s", out)
	}
}

// --- Dashboard op ---

func TestOpDashboard_ReturnsCampaignTable(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	svc.Proto.CreateArtifact(ctx, parchment.CreateInput{
		Labels: []string{"kind:effort.campaign"}, Title: "Test Campaign",
	})
	op := service.Find("dashboard")
	raw, _ := json.Marshal(map[string]any{})
	out, err := op.Run(ctx, svc, raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "SCOPE") || !strings.Contains(out, "CAMPAIGN") {
		t.Errorf("expected table headers, got: %s", out)
	}
	if !strings.Contains(out, "Test Campaign") {
		t.Errorf("expected campaign in output, got: %s", out)
	}
}

// --- Delete op ---

func TestOpDelete_SingleByID(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	art, _ := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{
		Labels: []string{"kind:knowledge.note"}, Title: "To delete",
	})
	op := service.Find("delete")
	raw, _ := json.Marshal(map[string]any{"id": art.ID})
	out, err := op.Run(ctx, svc, raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "deleted 1") {
		t.Errorf("expected 'deleted 1', got: %s", out)
	}
	_, err = svc.Proto.GetArtifact(ctx, art.ID)
	if err == nil {
		t.Error("artifact should be deleted")
	}
}

func TestOpDelete_DryRunDoesNotDelete(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	art, _ := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{
		Labels: []string{"kind:knowledge.note"}, Title: "Keep me",
	})
	op := service.Find("delete")
	raw, _ := json.Marshal(map[string]any{"id": art.ID, "dry_run": true})
	out, err := op.Run(ctx, svc, raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "dry run") {
		t.Errorf("expected dry run output, got: %s", out)
	}
	got, err := svc.Proto.GetArtifact(ctx, art.ID)
	if err != nil || got == nil {
		t.Error("artifact should NOT be deleted in dry run")
	}
}

func TestOpDelete_BulkByScope(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	for i := range 3 {
		svc.Proto.CreateArtifact(ctx, parchment.CreateInput{
			Labels: []string{"kind:knowledge.note", "project:bulk-test"},
			Title:  "Bulk " + string(rune('A'+i)),
		})
	}
	op := service.Find("delete")
	raw, _ := json.Marshal(map[string]any{"scope": "bulk-test", "kind": "knowledge.note"})
	out, err := op.Run(ctx, svc, raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "deleted 3") {
		t.Errorf("expected 'deleted 3', got: %s", out)
	}
}

func TestOpDelete_RequiresFilters(t *testing.T) {
	svc := newTestService(t)
	op := service.Find("delete")
	raw, _ := json.Marshal(map[string]any{})
	_, err := op.Run(context.Background(), svc, raw)
	if err == nil {
		t.Fatal("expected error for empty delete")
	}
}
