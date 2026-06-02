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
	// When Find("list") is called
	// Then the Op is returned
	if service.Find("list") == nil {
		t.Fatal("Find(\"list\") returned nil — list Op not registered")
	}
}

// --- SCR-TSK-387: opSet.Run ---

func TestOpSet_SetsSingleField(t *testing.T) {
	// Given an artifact exists with title "old"
	// When set(id=X, field=title, value=new) is called
	// Then the output contains "X.title = new"
	svc := newTestService(t)
	ctx := context.Background()

	art, err := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{
		Kind: "task", Title: "old", Scope: "test",
	})
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

	a, _ := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{Kind: "task", Title: "A", Scope: "test"})
	b, _ := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{Kind: "task", Title: "B", Scope: "test"})

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

	art, _ := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{
		Kind: "task", Title: "T", Scope: "test",
	})
	op := service.Find("set")
	raw, _ := json.Marshal(map[string]any{
		"id": art.ID, "field": "status", "value": "complete",
	})
	out, err := op.Run(ctx, svc, raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "error") {
		t.Errorf("expected error in output for invalid transition draft→complete, got: %s", out)
	}
}

// --- bulk_section_update (RED) ---

// --- archive (RED) ---

func TestOpArchive_SingleID(t *testing.T) {
	// Given a task in draft status
	// When archive(id=X) is called
	// Then the artifact status is archived
	svc := newTestService(t)
	ctx := context.Background()

	art, _ := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{Kind: "task", Title: "T", Scope: "test"})

	op := service.Find("archive")
	if op == nil {
		t.Fatal("archive Op not registered")
	}
	raw, _ := json.Marshal(map[string]any{"id": art.ID})
	out, err := op.Run(ctx, svc, raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "archived") {
		t.Errorf("expected 'archived' in output, got: %s", out)
	}
	updated, _ := svc.Proto.GetArtifact(ctx, art.ID)
	if updated.Status != "archived" {
		t.Errorf("status = %q, want archived", updated.Status)
	}
}

func TestOpArchive_BulkFilterByScope(t *testing.T) {
	// Given two tasks in scope "alpha", one in "beta"
	// When archive(scope=alpha) is called with no explicit IDs
	// Then both alpha tasks are archived, beta is untouched
	svc := newTestService(t)
	ctx := context.Background()

	a, _ := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{Kind: "task", Title: "A", Scope: "alpha"})
	b, _ := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{Kind: "task", Title: "B", Scope: "alpha"})
	c, _ := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{Kind: "task", Title: "C", Scope: "beta"})

	op := service.Find("archive")
	raw, _ := json.Marshal(map[string]any{"scope": "alpha"})
	_, err := op.Run(ctx, svc, raw)
	if err != nil {
		t.Fatal(err)
	}

	artA, _ := svc.Proto.GetArtifact(ctx, a.ID)
	artB, _ := svc.Proto.GetArtifact(ctx, b.ID)
	artC, _ := svc.Proto.GetArtifact(ctx, c.ID)

	if artA.Status != "archived" {
		t.Errorf("A.status = %q, want archived", artA.Status)
	}
	if artB.Status != "archived" {
		t.Errorf("B.status = %q, want archived", artB.Status)
	}
	if artC.Status == "archived" {
		t.Error("C should not be archived (different scope)")
	}
}

func TestOpArchive_DryRunNoMutation(t *testing.T) {
	// Given tasks exist in scope "test"
	// When archive(scope=test, dry_run=true) is called
	// Then output mentions count but no artifacts are archived
	svc := newTestService(t)
	ctx := context.Background()

	art, _ := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{Kind: "task", Title: "T", Scope: "test"})

	op := service.Find("archive")
	raw, _ := json.Marshal(map[string]any{"scope": "test", "dry_run": true})
	out, err := op.Run(ctx, svc, raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "dry") && !strings.Contains(out, "1") {
		t.Errorf("expected dry-run indication in output, got: %s", out)
	}
	unchanged, _ := svc.Proto.GetArtifact(ctx, art.ID)
	if unchanged.Status == "archived" {
		t.Error("artifact should not be archived in dry-run mode")
	}
}

// --- de-archive (RED) ---

func TestOpDeArchive_RestoresToDraft(t *testing.T) {
	// Given an archived artifact
	// When de-archive(id=X) is called
	// Then status returns to draft
	svc := newTestService(t)
	ctx := context.Background()

	art, _ := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{Kind: "task", Title: "T", Scope: "test"})
	svc.Proto.ArchiveArtifact(ctx, []string{art.ID}, false) //nolint:errcheck // test setup, error irrelevant to subject under test

	op := service.Find("de-archive")
	if op == nil {
		t.Fatal("de-archive Op not registered")
	}
	raw, _ := json.Marshal(map[string]any{"id": art.ID})
	out, err := op.Run(ctx, svc, raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "restored") {
		t.Errorf("expected 'restored' in output, got: %s", out)
	}
	updated, _ := svc.Proto.GetArtifact(ctx, art.ID)
	if updated.Status != "draft" {
		t.Errorf("status = %q, want draft", updated.Status)
	}
}

func TestOpDeArchive_MissingIDReturnsError(t *testing.T) {
	// Given no id provided
	// When de-archive() is called
	// Then an error is returned
	svc := newTestService(t)
	op := service.Find("de-archive")
	if op == nil {
		t.Fatal("de-archive Op not registered")
	}
	raw, _ := json.Marshal(map[string]any{})
	_, err := op.Run(context.Background(), svc, raw)
	if err == nil {
		t.Fatal("expected error for missing id, got nil")
	}
}

// --- retire (RED) ---

func TestOpRetire_TransitionsToRetired(t *testing.T) {
	// Given a task in draft status
	// When retire(id=X) is called
	// Then the artifact status is retired
	svc := newTestService(t)
	ctx := context.Background()

	art, _ := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{Kind: "task", Title: "T", Scope: "test"})

	op := service.Find("retire")
	if op == nil {
		t.Fatal("retire Op not registered")
	}
	raw, _ := json.Marshal(map[string]any{"id": art.ID})
	out, err := op.Run(ctx, svc, raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "retired") {
		t.Errorf("expected 'retired' in output, got: %s", out)
	}
	updated, _ := svc.Proto.GetArtifact(ctx, art.ID)
	if updated.Status != "retired" {
		t.Errorf("status = %q, want retired", updated.Status)
	}
}

func TestOpRetire_MultipleIDs(t *testing.T) {
	// Given two tasks exist
	// When retire(ids=[A,B]) is called
	// Then both appear in the output
	svc := newTestService(t)
	ctx := context.Background()

	a, _ := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{Kind: "task", Title: "A", Scope: "test"})
	b, _ := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{Kind: "task", Title: "B", Scope: "test"})

	op := service.Find("retire")
	raw, _ := json.Marshal(map[string]any{"ids": []string{a.ID, b.ID}})
	out, err := op.Run(ctx, svc, raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, a.ID) || !strings.Contains(out, b.ID) {
		t.Errorf("expected both IDs in output, got: %s", out)
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
		Kind: "note", Title: "authentication flow", Scope: "test",
	})

	op := service.Find("list")
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
	op := service.Find("list")
	raw, _ := json.Marshal(map[string]any{"ranked": true, "scope": "test"})
	_, err := op.Run(context.Background(), svc, raw)
	if err == nil {
		t.Fatal("expected error for ranked list with empty query, got nil")
	}
}

// --- diff (RED) ---

func TestOpOrient_ReturnsVaultReport(t *testing.T) {
	// Given notes exist in a scope
	// When orient(scope=test) is called
	// Then output contains vault structure sections
	svc := newTestService(t, "test")
	ctx := context.Background()

	svc.Proto.CreateArtifact(ctx, parchment.CreateInput{ //nolint:errcheck // test setup, error irrelevant to subject under test
		Kind: "note", Title: "design note", Scope: "test",
	})

	op := service.Find("orient")
	if op == nil {
		t.Fatal("orient Op not registered")
	}
	raw, _ := json.Marshal(map[string]any{"scope": "test"})
	out, err := op.Run(ctx, svc, raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Schema") && !strings.Contains(out, "Vault") {
		t.Errorf("expected vault structure report, got: %s", out[:min(200, len(out))])
	}
}

func TestOpUpdate_SetsMultipleFields(t *testing.T) {
	// Given a task exists
	// When update(id=X, title=new, priority=high) is called
	// Then both fields change
	svc := newTestService(t)
	ctx := context.Background()

	art, _ := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{Kind: "task", Title: "old", Scope: "test"})

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
	if updated.Priority != "high" {
		t.Errorf("priority = %q, want high", updated.Priority)
	}
}

func TestOpUpdate_AttachesSection(t *testing.T) {
	// Given a task exists
	// When update(id=X, sections=[{name:summary, text:body}]) is called
	// Then the section is attached
	svc := newTestService(t)
	ctx := context.Background()

	art, _ := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{Kind: "task", Title: "T", Scope: "test"})

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

	art, _ := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{Kind: "task", Title: "T", Scope: "test"})
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

	art, _ := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{Kind: "task", Title: "T", Scope: "test"})

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

	a, _ := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{Kind: "task", Title: "Alpha", Scope: "test"})
	b, _ := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{Kind: "task", Title: "Beta", Scope: "test"})

	op := service.Find("get")
	if op != nil {
		raw, _ := json.Marshal(map[string]any{"id": a.ID, "diff_against": b.ID})
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

	a, _ := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{Kind: "task", Title: "Alpha", Scope: "test"})
	b, _ := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{Kind: "task", Title: "Beta", Scope: "test"})

	op := service.Find("list")
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
		svc.Proto.CreateArtifact(ctx, parchment.CreateInput{Kind: "task", Title: "T", Scope: "test"}) //nolint:errcheck // test setup, error irrelevant to subject under test
	}
	op := service.Find("list")
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
		svc.Proto.CreateArtifact(ctx, parchment.CreateInput{Kind: "task", Title: "T", Scope: "test"}) //nolint:errcheck // test setup, error irrelevant to subject under test
	}
	op := service.Find("list")
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

	svc.Proto.CreateArtifact(ctx, parchment.CreateInput{Kind: "task", Title: "Compact", Scope: "test"}) //nolint:errcheck // test setup, error irrelevant to subject under test

	op := service.Find("list")
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
	op := service.Find("list")
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

	task, _ := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{Kind: "task", Title: "T", Scope: "test"})
	note, _ := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{Kind: "note", Title: "N", Scope: "test"})

	op := service.Find("list")
	raw, _ := json.Marshal(map[string]any{"kind": "task"})
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

	svc.Proto.CreateArtifact(ctx, parchment.CreateInput{Kind: "task", Title: "T", Scope: "test"}) //nolint:errcheck // test setup, error irrelevant to subject under test

	op := service.Find("list")
	raw, _ := json.Marshal(map[string]any{})
	out, err := op.Run(ctx, svc, raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "top=10") {
		t.Errorf("expected unfiltered hint containing top=10, got: %s", out)
	}
}
