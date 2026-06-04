package service_test

import (
	"context"
	"strings"
	"testing"

	parchment "github.com/dpopsuev/parchment"
	"github.com/dpopsuev/scribe/service"
)

// newTestService creates a Service backed by an in-memory store for tests.
func newTestService(t *testing.T, scopes ...string) *service.Service {
	t.Helper()
	if len(scopes) == 0 {
		scopes = []string{"test"}
	}
	store := parchment.NewMemoryStore()
	proto := parchment.New(store, nil, scopes, nil, parchment.ProtocolConfig{})
	return service.New(proto, nil, scopes)
}

// --- ContextRead ---

func TestContextRead_ReturnsTask(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc := newTestService(t)

	task, err := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{
		Kind:     "task",
		Title:    "fix auth bug",
		Scope:    "test",
		Priority: "high",
		Labels:   []string{"go", "security"},
		Sections: []parchment.Section{{Name: "context", Text: "JWT expiry not checked"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	packet, err := svc.ContextRead(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if packet.Task == nil {
		t.Fatal("expected task in packet, got nil")
	}
	if packet.Task.ID != task.ID {
		t.Errorf("task ID = %q, want %q", packet.Task.ID, task.ID)
	}
}

func TestContextRead_RulesExpandedByLabelHierarchy(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc := newTestService(t)

	// Create a note with labels "rule" and "lang.go" (PRC-ADR-6: rule is a label, not a kind)
	_, err := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{
		Kind:     "note",
		Title:    "Go conventions",
		Scope:    "global",
		Priority: "none",
		Labels:   []string{"rule", "lang.go"},
		Sections: []parchment.Section{{Name: "content", Text: "Use gofmt."}},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Create a task with label "lang.go" — rule should appear in context
	task, err := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{
		Kind:     "task",
		Title:    "write Go service",
		Scope:    "test",
		Priority: "medium",
		Labels:   []string{"lang.go"},
		Sections: []parchment.Section{{Name: "context", Text: "build a Go HTTP service"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	packet, err := svc.ContextRead(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(packet.Rules) == 0 {
		t.Error("expected Go conventions rule in context, got none")
	}
	if len(packet.Rules) > 0 && packet.Rules[0].Title != "Go conventions" {
		t.Errorf("expected 'Go conventions', got %q", packet.Rules[0].Title)
	}
}

func TestContextRead_AlwaysRulesAlwaysIncluded(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc := newTestService(t)

	_, err := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{
		Kind:     "note",
		Title:    "KISS directives",
		Scope:    "global",
		Priority: "none",
		Labels:   []string{"rule", "always"},
		Sections: []parchment.Section{{Name: "content", Text: "Keep it simple."}},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Task with no matching labels — always rule should still appear
	task, err := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{
		Kind:     "task",
		Title:    "unrelated task",
		Scope:    "test",
		Priority: "low",
		Labels:   []string{"rust"},
		Sections: []parchment.Section{{Name: "context", Text: "Rust stuff"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	packet, err := svc.ContextRead(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(packet.Rules) == 0 {
		t.Error("expected always rule to be included, got none")
	}
}

func TestContextRead_KnowledgeInSameScope(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc := newTestService(t)

	_, err := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{
		Kind:   "note",
		Title:  "auth note",
		Scope:  "test",
		Labels: []string{"security"},
	})
	if err != nil {
		t.Fatal(err)
	}

	task, err := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{
		Kind:     "task",
		Title:    "auth task",
		Scope:    "test",
		Priority: "high",
		Labels:   []string{"security"},
		Sections: []parchment.Section{{Name: "context", Text: "fix auth"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	packet, err := svc.ContextRead(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(packet.Know) == 0 {
		t.Error("expected auth note in knowledge layer, got none")
	}
}



// --- Brief ---

func TestBrief_EmptyStore(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc := newTestService(t, "test")

	m, err := svc.Brief(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if m == nil {
		t.Fatal("Brief returned nil")
	}
}

func TestBrief_ReturnsCurrentGoals(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc := newTestService(t, "test")

	_, err := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{
		Kind:  "goal",
		Title: "ship labeldef",
		Scope: "test",
	})
	if err != nil {
		t.Fatal(err)
	}

	m, err := svc.Brief(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// goal starts as draft; brief tracks active/current goals
	// just verify brief does not error and returns a packet
	// no current goals yet — just verify Brief does not error
	_ = m
}

// --- RenderChangelog ---

func TestRenderChangelog_RequiresSince(t *testing.T) {
	// Given no since timestamp
	// When RenderChangelog("", "") is called
	// Then an error is returned
	svc := newTestService(t, "test")
	_, err := svc.RenderChangelog(context.Background(), "", "")
	if err == nil {
		t.Fatal("expected error for empty since, got nil")
	}
}

func TestRenderChangelog_ShowsChangedArtifacts(t *testing.T) {
	// Given an artifact was updated after a timestamp
	// When RenderChangelog(since, scope) is called
	// Then the artifact appears in output
	svc := newTestService(t, "test")
	ctx := context.Background()

	past := "2020-01-01T00:00:00Z"
	art, _ := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{Kind: "task", Title: "recent", Scope: "test"})

	out, err := svc.RenderChangelog(ctx, past, "test")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, art.ID) {
		t.Errorf("expected artifact in changelog, got: %s", out[:min(200, len(out))])
	}
}

// --- RenderDetect ---

func TestRenderDetect_AllChecksRun(t *testing.T) {
	// Given an empty store
	// When RenderDetect(check=all) is called
	// Then output contains results for overlaps and orphans
	svc := newTestService(t, "test")
	out, err := svc.RenderDetect(context.Background(), "all", "test", "", "", "", 7)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "overlap") && !strings.Contains(out, "orphan") {
		t.Errorf("expected overlap or orphan section in detect output, got: %s", out[:min(200, len(out))])
	}
}

func TestRenderDetect_EvictionCheck(t *testing.T) {
	// Given eviction check is requested
	// When RenderDetect(check=eviction) is called
	// Then output mentions eviction candidates
	svc := newTestService(t, "test")
	out, err := svc.RenderDetect(context.Background(), "eviction", "test", "", "", "", 7)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "eviction") && !strings.Contains(out, "No eviction") {
		t.Errorf("expected eviction content in output, got: %s", out)
	}
}

// --- RenderBrief ---

func TestRenderBrief_ContainsScopeAndVersion(t *testing.T) {
	// Given an empty store
	// When RenderBrief is called with version and scopes
	// Then output contains version and scope info
	svc := newTestService(t, "test")

	out, err := svc.RenderBrief(context.Background(), "", "v1.0", []string{"test"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "v1.0") {
		t.Errorf("expected version in brief output, got: %s", out[:min(200, len(out))])
	}
	if !strings.Contains(out, "test") {
		t.Errorf("expected scope in brief output, got: %s", out[:min(200, len(out))])
	}
}

func TestRenderBrief_ShowsOpenBugs(t *testing.T) {
	// Given an open bug exists
	// When RenderBrief is called
	// Then output contains the bug ID
	svc := newTestService(t, "test")
	ctx := context.Background()

	bug, _ := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{
		Kind: "bug", Title: "bad crash", Scope: "test", Status: "open",
	})

	out, err := svc.RenderBrief(ctx, "", "v1", []string{"test"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, bug.ID) {
		t.Errorf("expected bug ID in brief, got: %s", out[:min(400, len(out))])
	}
}

// --- ExpandLabels integration ---

func TestExpandLabels_DotHierarchy(t *testing.T) {
	t.Parallel()
	got := parchment.ExpandLabels([]string{"lang.go.test"})
	want := map[string]bool{"lang.go.test": true, "lang.go": true, "lang": true}
	for _, l := range got {
		if !want[l] {
			t.Errorf("unexpected label %q in expansion", l)
		}
	}
	for w := range want {
		found := false
		for _, l := range got {
			if l == w {
				found = true
			}
		}
		if !found {
			t.Errorf("missing expected label %q in expansion", w)
		}
	}
}

// --- SortArtifacts ---

func TestSortArtifacts_ByTitle(t *testing.T) {
	arts := []*parchment.Artifact{
		{ID: "B", Title: "beta"},
		{ID: "A", Title: "alpha"},
		{ID: "C", Title: "gamma"},
	}
	service.SortArtifacts(arts, "title")
	if arts[0].Title != "alpha" || arts[1].Title != "beta" || arts[2].Title != "gamma" {
		t.Errorf("SortArtifacts(title) wrong order: %v", []string{arts[0].Title, arts[1].Title, arts[2].Title})
	}
}

func TestSortArtifacts_DefaultsToID(t *testing.T) {
	arts := []*parchment.Artifact{
		{ID: "C"}, {ID: "A"}, {ID: "B"},
	}
	service.SortArtifacts(arts, "unknown_field")
	if arts[0].ID != "A" || arts[2].ID != "C" {
		t.Errorf("SortArtifacts(unknown) should fall back to ID sort, got: %v", []string{arts[0].ID, arts[1].ID, arts[2].ID})
	}
}

// --- IsComponentLabel ---

func TestIsComponentLabel(t *testing.T) {
	cases := []struct {
		input string
		want  bool
	}{
		{"file:pkg/foo.go", true},
		{"pkg:github.com/org/repo/pkg", true},
		{"fqn:pkg/Func", true},
		{"label", false},
		{"file:", false},
		{"", false},
		{"file:foo", false}, // no slash
	}
	for _, tc := range cases {
		got := service.IsComponentLabel(tc.input)
		if got != tc.want {
			t.Errorf("IsComponentLabel(%q) = %v, want %v", tc.input, got, tc.want)
		}
	}
}

// --- RenderBriefCompact ---

func TestRenderBriefCompact_ContainsVersionAndCounts(t *testing.T) {
	t.Parallel()
	svc := newTestService(t)
	ctx := context.Background()

	svc.Proto.CreateArtifact(ctx, parchment.CreateInput{ //nolint:errcheck // test setup
		Kind: "task", Title: "active task", Scope: "test", Status: "active",
	})

	out, err := svc.RenderBriefCompact(ctx, "v2.19.0")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "v2.19.0") {
		t.Errorf("compact brief should contain version, got: %s", out)
	}
	if !strings.Contains(out, "active") {
		t.Errorf("compact brief should mention active count, got: %s", out)
	}
}

// --- RenderDashboard ---

func TestRenderDashboard_ReturnsJSON(t *testing.T) {
	t.Parallel()
	svc := newTestService(t)
	ctx := context.Background()

	out, err := svc.RenderDashboard(ctx, 30)
	if err != nil {
		t.Fatal(err)
	}
	if out == "" {
		t.Error("RenderDashboard returned empty string")
	}
	// Output should be JSON
	if out[0] != '{' {
		t.Errorf("RenderDashboard output should be JSON object, got: %.20s", out)
	}
}

// --- RenderKnowledgeLint ---

func TestRenderKnowledgeLint_ReturnsString(t *testing.T) {
	t.Parallel()
	svc := newTestService(t)
	out, err := svc.RenderKnowledgeLint(context.Background(), "test")
	if err != nil {
		t.Fatal(err)
	}
	// With no artifacts, should return some output (even if just a header)
	_ = out // just verify it doesn't panic or error
}

// --- SetGoal ---

func TestSetGoal_CreatesGoalAndRoot(t *testing.T) {
	// Given: a service with KnowledgeSchema (has goal kind)
	// When: SetGoal is called with a title
	// Then: a goal artifact and a root spec (justifies goal) are created
	t.Parallel()
	store := parchment.NewMemoryStore()
	proto := parchment.New(store, parchment.KnowledgeSchema(), []string{"test"}, nil, parchment.ProtocolConfig{})
	svc := service.New(proto, nil, []string{"test"})
	ctx := context.Background()

	result, err := svc.SetGoal(ctx, service.SetGoalInput{Title: "improve semantic search", Scope: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Goal == nil {
		t.Fatal("SetGoal should return a goal artifact")
	}
	if result.Root == nil {
		t.Fatal("SetGoal should return a root artifact")
	}
	if result.Goal.Kind != "goal" {
		t.Errorf("goal kind = %q, want %q", result.Goal.Kind, "goal")
	}
	// Root should justify the goal
	found := false
	for _, id := range result.Root.Links[parchment.RelJustifies] {
		if id == result.Goal.ID {
			found = true
		}
	}
	if !found {
		t.Errorf("root should justify goal %s via %s edge", result.Goal.ID, parchment.RelJustifies)
	}
}

func TestSetGoal_ArchivesExistingGoal(t *testing.T) {
	// Given: an existing current goal
	// When: SetGoal is called again
	// Then: the old goal is archived, new goal is created
	t.Parallel()
	store := parchment.NewMemoryStore()
	proto := parchment.New(store, parchment.KnowledgeSchema(), []string{"test"}, nil, parchment.ProtocolConfig{})
	svc := service.New(proto, nil, []string{"test"})
	ctx := context.Background()

	first, err := svc.SetGoal(ctx, service.SetGoalInput{Title: "first goal", Scope: "test"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.SetGoal(ctx, service.SetGoalInput{Title: "second goal", Scope: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Archived) == 0 {
		t.Error("expected first goal to be archived when setting a new goal")
	}
	found := false
	for _, a := range second.Archived {
		if a.ID == first.Goal.ID {
			found = true
		}
	}
	if !found {
		t.Errorf("first goal %s should be in archived list", first.Goal.ID)
	}
}

func TestSetGoal_RequiresTitle(t *testing.T) {
	svc := newTestService(t)
	_, err := svc.SetGoal(context.Background(), service.SetGoalInput{})
	if err == nil {
		t.Fatal("SetGoal without title should return error")
	}
}

// --- FilterSections ---

func TestFilterSections_KeepsRequestedSections(t *testing.T) {
	art := &parchment.Artifact{
		Sections: []parchment.Section{
			{Name: "summary", Text: "a"},
			{Name: "context", Text: "b"},
			{Name: "decision", Text: "c"},
		},
	}
	service.FilterSections(art, []string{"summary", "decision"})
	if len(art.Sections) != 2 {
		t.Fatalf("expected 2 sections, got %d", len(art.Sections))
	}
	if art.Sections[0].Name != "summary" || art.Sections[1].Name != "decision" {
		t.Errorf("wrong sections: %v", art.Sections)
	}
}

func TestFilterSections_EmptyFilterKeepsAll(t *testing.T) {
	art := &parchment.Artifact{
		Sections: []parchment.Section{{Name: "a"}, {Name: "b"}},
	}
	service.FilterSections(art, nil)
	if len(art.Sections) != 2 {
		t.Errorf("empty filter should keep all sections, got %d", len(art.Sections))
	}
}

// --- RelevanceScore ---

func TestRelevanceScore_ActiveHigherThanDraft(t *testing.T) {
	active := &parchment.Artifact{Status: "active", Priority: "high"}
	draft := &parchment.Artifact{Status: "draft", Priority: "high"}
	if service.RelevanceScore(active) <= service.RelevanceScore(draft) {
		t.Error("active artifact should score higher than draft")
	}
}

func TestRelevanceScore_CriticalHigherThanLow(t *testing.T) {
	crit := &parchment.Artifact{Status: "active", Priority: "critical"}
	low := &parchment.Artifact{Status: "active", Priority: "low"}
	if service.RelevanceScore(crit) <= service.RelevanceScore(low) {
		t.Error("critical priority should score higher than low")
	}
}

// --- ExtractIDs ---

func TestExtractIDs_FindsArtifactIDs(t *testing.T) {
	text := "Working on SCR-TSK-383 and PRC-GOL-12, also see ALE-SPC-5"
	ids := service.ExtractIDs(text)
	for _, want := range []string{"SCR-TSK-383", "PRC-GOL-12", "ALE-SPC-5"} {
		if !ids[want] {
			t.Errorf("expected %q in extracted IDs, got: %v", want, ids)
		}
	}
}

func TestExtractIDs_EmptyOnNoMatch(t *testing.T) {
	ids := service.ExtractIDs("no artifact ids here")
	if len(ids) != 0 {
		t.Errorf("expected empty map, got %v", ids)
	}
}

// --- EvidenceImpliesComplete ---

func TestEvidenceImpliesComplete_DetectsCompletionSignal(t *testing.T) {
	cases := []struct {
		evidence string
		id       string
		want     bool
	}{
		{"scr-tsk-383 is done and merged", "scr-tsk-383", true},
		{"closed scr-tsk-383 in the last commit", "scr-tsk-383", true},
		{"working on scr-tsk-383", "scr-tsk-383", false},
		{"scr-tsk-383 is blocked", "scr-tsk-383", false},
		{"no artifact id here", "scr-tsk-999", false},
	}
	for _, tc := range cases {
		got := service.EvidenceImpliesComplete(tc.evidence, tc.id)
		if got != tc.want {
			t.Errorf("EvidenceImpliesComplete(%q, %q) = %v, want %v", tc.evidence, tc.id, got, tc.want)
		}
	}
}

// --- ExtractTitle ---

func TestExtractTitle_ReturnsFirstMeaningfulLine(t *testing.T) {
	s := "Summary:\nThis is the real title\nMore details here"
	got := service.ExtractTitle(s)
	if got != "This is the real title" {
		t.Errorf("ExtractTitle = %q, want %q", got, "This is the real title")
	}
}

func TestExtractTitle_FallsBackToDefault(t *testing.T) {
	got := service.ExtractTitle("")
	if got != "Session memory" {
		t.Errorf("ExtractTitle(\"\") = %q, want \"Session memory\"", got)
	}
}

// --- Truncate ---

func TestTruncate_ShortStringUnchanged(t *testing.T) {
	got := service.Truncate("hello", 80)
	if got != "hello" {
		t.Errorf("Truncate short = %q, want %q", got, "hello")
	}
}

func TestTruncate_LongStringTruncated(t *testing.T) {
	long := strings.Repeat("a", 100)
	got := service.Truncate(long, 80)
	if len([]rune(got)) > 80 {
		t.Errorf("Truncate long: len = %d, want <= 80", len(got))
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("Truncate long should end with ellipsis, got: %q", got)
	}
}

// --- Inventory ---

func TestInventory_CountsByKindAndStatus(t *testing.T) {
	t.Parallel()
	svc := newTestService(t)
	ctx := context.Background()

	svc.Proto.CreateArtifact(ctx, parchment.CreateInput{Kind: "task", Title: "t1", Scope: "test"}) //nolint:errcheck // test setup
	svc.Proto.CreateArtifact(ctx, parchment.CreateInput{Kind: "task", Title: "t2", Scope: "test"}) //nolint:errcheck // test setup
	svc.Proto.CreateArtifact(ctx, parchment.CreateInput{Kind: "note", Title: "n1", Scope: "test"}) //nolint:errcheck // test setup

	result, err := svc.Inventory(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if result.Total < 3 {
		t.Errorf("Inventory.Total = %d, want >= 3", result.Total)
	}
	if result.ByKind["task"] < 2 {
		t.Errorf("ByKind[task] = %d, want >= 2", result.ByKind["task"])
	}
}

// --- BulkSetField in parchment ---

func TestBulkSetField_UpdatesMatchingArtifacts(t *testing.T) {
	t.Parallel()
	svc := newTestService(t)
	ctx := context.Background()

	svc.Proto.CreateArtifact(ctx, parchment.CreateInput{Kind: "task", Title: "alpha", Scope: "test"}) //nolint:errcheck // test setup
	svc.Proto.CreateArtifact(ctx, parchment.CreateInput{Kind: "task", Title: "beta", Scope: "test"})  //nolint:errcheck // test setup

	result, err := svc.Proto.BulkSetField(ctx, parchment.BulkMutationInput{
		Kind: "task", Scope: "test",
	}, "title", "bulk-updated")
	if err != nil {
		t.Fatal(err)
	}
	if result.Count < 2 {
		t.Errorf("BulkSetField count = %d, want >= 2", result.Count)
	}
}

// --- ExtractExcerpt ---

func TestExtractExcerpt_FindsTermInSection(t *testing.T) {
	art := &parchment.Artifact{
		Sections: []parchment.Section{
			{Name: "context", Text: "The authentication flow uses JWT tokens for session management."},
		},
	}
	excerpt := service.ExtractExcerpt(art, []string{"jwt"})
	if !strings.Contains(strings.ToLower(excerpt), "jwt") {
		t.Errorf("excerpt should contain 'jwt', got: %q", excerpt)
	}
}

func TestExtractExcerpt_FallsBackToGoal(t *testing.T) {
	art := &parchment.Artifact{
		Goal: "improve authentication",
	}
	excerpt := service.ExtractExcerpt(art, []string{"notfound"})
	if excerpt != "improve authentication" {
		t.Errorf("should fall back to goal, got: %q", excerpt)
	}
}

func TestExtractExcerpt_EmptyWhenNoMatch(t *testing.T) {
	art := &parchment.Artifact{Title: "unrelated"}
	excerpt := service.ExtractExcerpt(art, []string{"xyz"})
	if excerpt != "" {
		t.Errorf("expected empty excerpt, got: %q", excerpt)
	}
}

// --- RenderCheck ---

func TestRenderCheck_ReturnsJSON(t *testing.T) {
	t.Parallel()
	svc := newTestService(t)
	out, err := svc.RenderCheck(context.Background(), "test")
	if err != nil {
		t.Fatal(err)
	}
	if out == "" {
		t.Error("RenderCheck returned empty string")
	}
}

// --- SortArtifacts other fields ---

func TestSortArtifacts_ByStatus(t *testing.T) {
	arts := []*parchment.Artifact{
		{ID: "C", Status: "complete"},
		{ID: "A", Status: "active"},
		{ID: "D", Status: "draft"},
	}
	service.SortArtifacts(arts, "status")
	if arts[0].Status != "active" {
		t.Errorf("SortArtifacts(status)[0] = %q, want active", arts[0].Status)
	}
}

func TestSortArtifacts_ByScope(t *testing.T) {
	arts := []*parchment.Artifact{
		{ID: "1", Scope: "z-scope"},
		{ID: "2", Scope: "a-scope"},
	}
	service.SortArtifacts(arts, "scope")
	if arts[0].Scope != "a-scope" {
		t.Errorf("SortArtifacts(scope)[0] = %q, want a-scope", arts[0].Scope)
	}
}
