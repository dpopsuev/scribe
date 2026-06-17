package integration_test

import (
	"context"
	"testing"

	parchment "github.com/dpopsuev/parchment"
	"github.com/dpopsuev/scribe/internal/ingest"
	"github.com/dpopsuev/scribe/service"
	"github.com/dpopsuev/scribe/testkit"
)

func TestE2E_IngestWithRef_Resolve(t *testing.T) {
	db := testkit.NewStore(t)
	ctx := context.Background()

	nodes := []ingest.NodeRecord{
		{
			Type:   "node",
			ID:     "jira:AUTH-42",
			Kind:   "intent.bug",
			Title:  "Auth token expiry bug",
			Labels: []string{"source:emcee", "project:auth"},
			Extra: map[string]any{
				"ref_backend": "emcee",
				"ref_id":      "jira:AUTH-42",
				"status":      "open",
				"priority":    "high",
				"assignee":    "daniel",
			},
			Sections: []ingest.Section{
				{Name: "components", Text: "auth, token-service"},
			},
		},
	}
	edges := []ingest.EdgeRecord{
		{Type: "edge", From: "jira:AUTH-42", To: "jira:AUTH-7", Relation: "blocks"},
	}

	result, err := ingest.Apply(ctx, db, "emcee", nodes, edges)
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	if result.Inserted != 1 {
		t.Fatalf("inserted=%d, want 1; errors: %v", result.Inserted, result.Errors)
	}

	// Verify artifact is stored with ref fields in Extra
	art, err := db.Get(ctx, "jira:AUTH-42")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if service.RefBackend(art) != "emcee" {
		t.Errorf("RefBackend=%q, want emcee", service.RefBackend(art))
	}
	if service.RefID(art) != "jira:AUTH-42" {
		t.Errorf("RefID=%q, want jira:AUTH-42", service.RefID(art))
	}

	// Verify resolve returns cached data (no live resolver)
	proto := parchment.New(db, nil, []string{"auth"}, nil, parchment.ProtocolConfig{})
	svc := service.New(proto, nil, []string{"auth"})
	resolved, err := service.Resolve(ctx, svc, "jira:AUTH-42", nil)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if resolved.Fresh {
		t.Error("expected fresh=false (no resolver)")
	}
	if len(resolved.Sections) != 1 || resolved.Sections[0].Name != "components" {
		t.Errorf("expected cached sections, got: %v", resolved.Sections)
	}
}

func TestE2E_IngestValidation_RejectsInvalid(t *testing.T) {
	db := testkit.NewStore(t)
	ctx := context.Background()
	schemas := parchment.LoadSourceSchemas()

	nodes := []ingest.NodeRecord{
		{
			Type:  "node",
			ID:    "bad-record",
			Kind:  "intent.bug",
			Title: "Missing ref fields",
			Extra: map[string]any{
				"status": "open",
			},
		},
	}

	result, err := ingest.Apply(ctx, db, "emcee", nodes, nil, schemas)
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	if result.Inserted != 0 {
		t.Errorf("inserted=%d, want 0 (validation should reject)", result.Inserted)
	}
	if len(result.Errors) == 0 {
		t.Error("expected validation errors for missing ref_backend/ref_id")
	}
}

func TestE2E_Staleness(t *testing.T) {
	db := testkit.NewStore(t)
	ctx := context.Background()

	nodes := []ingest.NodeRecord{
		{
			Type:  "node",
			ID:    "jira:STALE-1",
			Kind:  "intent.bug",
			Title: "Old bug",
			Extra: map[string]any{
				"ref_backend": "emcee",
				"ref_id":      "jira:STALE-1",
			},
		},
	}

	result, err := ingest.Apply(ctx, db, "emcee", nodes, nil)
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	if result.Inserted != 1 {
		t.Fatalf("inserted=%d, want 1", result.Inserted)
	}

	art, err := db.Get(ctx, "jira:STALE-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}

	// Freshly inserted — should NOT be stale
	if service.IsStale(art) {
		t.Error("freshly inserted artifact should not be stale")
	}
}
