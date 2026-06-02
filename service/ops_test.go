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

func TestOpBulkSectionUpdate_ReplacesTextInSections(t *testing.T) {
	// Given an artifact has a section containing "old text"
	// When bulk_section_update(id=X, query=old text, text=new text) is called
	// Then the section body contains "new text" instead
	svc := newTestService(t)
	ctx := context.Background()

	art, _ := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{Kind: "note", Title: "N", Scope: "test"})
	svc.Proto.AttachSection(ctx, art.ID, "body", "old text in here") //nolint:errcheck // test setup, error irrelevant to subject under test

	op := service.Find("bulk_section_update")
	if op == nil {
		t.Fatal("bulk_section_update Op not registered")
	}
	raw, _ := json.Marshal(map[string]any{"id": art.ID, "query": "old text", "text": "new text"})
	out, err := op.Run(ctx, svc, raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "1") {
		t.Errorf("expected updated count in output, got: %s", out)
	}
	updated, _ := svc.Proto.GetArtifact(ctx, art.ID)
	if len(updated.Sections) == 0 || !strings.Contains(updated.Sections[0].Text, "new text") {
		t.Errorf("section text not updated, got: %v", updated.Sections)
	}
}

func TestOpBulkSectionUpdate_NoMatchIsNoop(t *testing.T) {
	// Given an artifact has a section with no matching text
	// When bulk_section_update(id=X, query=nonexistent, text=x) is called
	// Then output reports 0 updates
	svc := newTestService(t)
	ctx := context.Background()

	art, _ := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{Kind: "note", Title: "N", Scope: "test"})
	svc.Proto.AttachSection(ctx, art.ID, "body", "something else entirely") //nolint:errcheck // test setup, error irrelevant to subject under test

	op := service.Find("bulk_section_update")
	raw, _ := json.Marshal(map[string]any{"id": art.ID, "query": "nonexistent", "text": "x"})
	out, err := op.Run(ctx, svc, raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "0") {
		t.Errorf("expected 0 updates in output, got: %s", out)
	}
}

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

func TestOpRecall_ReturnsMatchingArtifacts(t *testing.T) {
	// Given a note about "authentication" exists
	// When recall(query=authentication) is called
	// Then the note appears in results
	svc := newTestService(t)
	ctx := context.Background()

	svc.Proto.CreateArtifact(ctx, parchment.CreateInput{ //nolint:errcheck // test setup, error irrelevant to subject under test
		Kind: "note", Title: "authentication flow", Scope: "test",
	})

	op := service.Find("recall")
	if op == nil {
		t.Fatal("recall Op not registered")
	}
	raw, _ := json.Marshal(map[string]any{"query": "authentication", "scope": "test"})
	out, err := op.Run(ctx, svc, raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "authentication") {
		t.Errorf("expected 'authentication' in recall output, got: %s", out)
	}
}

func TestOpRecall_EmptyQueryReturnsError(t *testing.T) {
	// Given no query is provided
	// When recall() is called
	// Then an error is returned
	svc := newTestService(t)
	op := service.Find("recall")
	if op == nil {
		t.Fatal("recall Op not registered")
	}
	raw, _ := json.Marshal(map[string]any{"scope": "test"})
	_, err := op.Run(context.Background(), svc, raw)
	if err == nil {
		t.Fatal("expected error for empty query, got nil")
	}
}

// --- diff (RED) ---

func TestOpDiff_DetectsFieldChange(t *testing.T) {
	// Given two artifacts with different titles
	// When diff(id=A, against=B) is called
	// Then output contains "title" and both values
	svc := newTestService(t)
	ctx := context.Background()

	a, _ := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{Kind: "task", Title: "Alpha", Scope: "test"})
	b, _ := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{Kind: "task", Title: "Beta", Scope: "test"})

	op := service.Find("diff")
	if op == nil {
		t.Fatal("diff Op not registered")
	}
	raw, _ := json.Marshal(map[string]any{"id": a.ID, "against": b.ID})
	out, err := op.Run(ctx, svc, raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "title") {
		t.Errorf("expected 'title' in diff output, got: %s", out)
	}
}

func TestOpDiff_NoDifferencesReturnsCleanMessage(t *testing.T) {
	// Given two artifacts with identical fields
	// When diff(id=A, against=A) is called
	// Then output indicates no differences
	svc := newTestService(t)
	ctx := context.Background()

	a, _ := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{Kind: "task", Title: "Same", Scope: "test"})

	op := service.Find("diff")
	if op == nil {
		t.Fatal("diff Op not registered")
	}
	raw, _ := json.Marshal(map[string]any{"id": a.ID, "against": a.ID})
	out, err := op.Run(ctx, svc, raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "no diff") {
		t.Errorf("expected 'no diff' message, got: %s", out)
	}
}

// --- detach_section (RED) ---

func TestOpDetachSection_RemovesSection(t *testing.T) {
	// Given an artifact has a section named "summary"
	// When detach_section(id=X, name=summary) is called
	// Then get(X).Sections no longer contains "summary"
	svc := newTestService(t)
	ctx := context.Background()

	art, _ := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{Kind: "note", Title: "N", Scope: "test"})
	_, _ = svc.Proto.AttachSection(ctx, art.ID, "summary", "text here")

	op := service.Find("detach_section")
	if op == nil {
		t.Fatal("detach_section Op not registered")
	}
	raw, _ := json.Marshal(map[string]any{"id": art.ID, "name": "summary"})
	out, err := op.Run(ctx, svc, raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "removed") {
		t.Errorf("expected 'removed' in output, got: %s", out)
	}
	updated, _ := svc.Proto.GetArtifact(ctx, art.ID)
	for _, sec := range updated.Sections {
		if sec.Name == "summary" {
			t.Error("section 'summary' still present after detach")
		}
	}
}

func TestOpDetachSection_NotFoundReturnsMessage(t *testing.T) {
	// Given an artifact has no section named "missing"
	// When detach_section(id=X, name=missing) is called
	// Then output indicates not found (no error)
	svc := newTestService(t)
	ctx := context.Background()

	art, _ := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{Kind: "note", Title: "N", Scope: "test"})

	op := service.Find("detach_section")
	if op == nil {
		t.Fatal("detach_section Op not registered")
	}
	raw, _ := json.Marshal(map[string]any{"id": art.ID, "name": "missing"})
	out, err := op.Run(ctx, svc, raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "not found") {
		t.Errorf("expected 'not found' in output, got: %s", out)
	}
}

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
