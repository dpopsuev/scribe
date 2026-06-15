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
	active := &parchment.Artifact{Labels: []string{"work.active", "priority:high"}}
	draft := &parchment.Artifact{Labels: []string{"work.draft", "priority:high"}}
	if service.RelevanceScore(active) <= service.RelevanceScore(draft) {
		t.Error("active artifact should score higher than draft")
	}
}

func TestRelevanceScore_CriticalHigherThanLow(t *testing.T) {
	crit := &parchment.Artifact{Labels: []string{"work.active", "priority:critical"}}
	low := &parchment.Artifact{Labels: []string{"work.active", "priority:low"}}
	if service.RelevanceScore(crit) <= service.RelevanceScore(low) {
		t.Error("critical priority should score higher than low")
	}
}

// --- Inventory ---

func TestInventory_CountsByKindAndStatus(t *testing.T) {
	t.Parallel()
	svc := newTestService(t)
	ctx := context.Background()

	svc.Proto.CreateArtifact(ctx, parchment.CreateInput{Labels: []string{"kind:effort.task"}, Title: "t1"})    //nolint:errcheck // test setup
	svc.Proto.CreateArtifact(ctx, parchment.CreateInput{Labels: []string{"kind:effort.task"}, Title: "t2"})    //nolint:errcheck // test setup
	svc.Proto.CreateArtifact(ctx, parchment.CreateInput{Labels: []string{"kind:knowledge.note"}, Title: "n1"}) //nolint:errcheck // test setup

	result, err := svc.Inventory(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if result.Total < 3 {
		t.Errorf("Inventory.Total = %d, want >= 3", result.Total)
	}
	if result.ByKind["effort.task"] < 2 {
		t.Errorf("ByKind[effort.task] = %d, want >= 2", result.ByKind["effort.task"])
	}
}

// --- BulkSetField in parchment ---

func TestBulkSetField_UpdatesMatchingArtifacts(t *testing.T) {
	t.Parallel()
	svc := newTestService(t)
	ctx := context.Background()

	svc.Proto.CreateArtifact(ctx, parchment.CreateInput{Labels: []string{"kind:effort.task"}, Title: "alpha"}) //nolint:errcheck // test setup
	svc.Proto.CreateArtifact(ctx, parchment.CreateInput{Labels: []string{"kind:effort.task"}, Title: "beta"})  //nolint:errcheck // test setup

	result, err := svc.Proto.BulkSetField(ctx, parchment.BulkMutationInput{
		Labels: []string{"kind:effort.task"},
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
		Sections: []parchment.Section{{Name: "goal", Text: "improve authentication"}},
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

// --- SortArtifacts other fields ---

func TestSortArtifacts_ByStatus(t *testing.T) {
	arts := []*parchment.Artifact{
		{ID: "C", Labels: []string{"work.complete"}},
		{ID: "A", Labels: []string{"work.active"}},
		{ID: "D", Labels: []string{"work.draft"}},
	}
	service.SortArtifacts(arts, "status")
	if parchment.StatusFromLabels(arts[0].Labels) != "work.active" {
		t.Errorf("SortArtifacts(status)[0] = %q, want work.active", parchment.StatusFromLabels(arts[0].Labels))
	}
}

func TestSortArtifacts_ByScope(t *testing.T) {
	arts := []*parchment.Artifact{
		{ID: "1", Labels: []string{"project:z-scope"}},
		{ID: "2", Labels: []string{"project:a-scope"}},
	}
	service.SortArtifacts(arts, "scope")
	if arts[0].Label(parchment.LabelPrefixScope) != "a-scope" {
		t.Errorf("SortArtifacts(scope)[0] = %q, want a-scope", arts[0].Label(parchment.LabelPrefixScope))
	}
}
