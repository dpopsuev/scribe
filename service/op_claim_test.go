package service_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	parchment "github.com/dpopsuev/parchment"
	"github.com/dpopsuev/scribe/service"
)

func TestClaimRelease_RaceAndExpiry(t *testing.T) {
	svc := newTestService(t, "claim")
	ctx := context.Background()

	art, err := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{
		Labels:   []string{"kind:effort.task", "scope:claim", "work.draft", "priority:medium"},
		Title:    "Claimed task",
		Sections: []parchment.Section{{Name: "context", Text: "x"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	claim := service.Find("claim")
	raw, _ := json.Marshal(map[string]any{"id": art.ID, "agent": "agent-a", "ttl_seconds": 3600})
	if _, err := claim.Run(ctx, svc, raw); err != nil {
		t.Fatal(err)
	}

	raw2, _ := json.Marshal(map[string]any{"id": art.ID, "agent": "agent-b", "ttl_seconds": 3600})
	if _, err := claim.Run(ctx, svc, raw2); err == nil {
		t.Fatal("expected second claim to fail")
	}

	set := service.Find("set")
	rawSet, _ := json.Marshal(map[string]any{"id": art.ID, "field": "title", "value": "Nope"})
	if _, err := set.Run(ctx, svc, rawSet); err == nil {
		t.Fatal("expected set to fail while claimed")
	}

	release := service.Find("release")
	rawRel, _ := json.Marshal(map[string]any{"id": art.ID, "agent": "agent-a"})
	if _, err := release.Run(ctx, svc, rawRel); err != nil {
		t.Fatal(err)
	}
	if _, err := set.Run(ctx, svc, rawSet); err != nil {
		t.Fatalf("set after release: %v", err)
	}

	// Expired claim allows reclaim
	expired := parchment.Claim{Agent: "agent-a", ExpiresAt: time.Now().Add(-time.Minute)}
	got, _ := svc.Proto.GetArtifact(ctx, art.ID)
	got.Extra = parchment.ApplyClaim(got.Extra, expired)
	if err := svc.Proto.UpdateArtifact(ctx, got, got.UpdatedAt); err != nil {
		t.Fatal(err)
	}
	if _, err := claim.Run(ctx, svc, raw2); err != nil {
		t.Fatalf("reclaim after expiry: %v", err)
	}
}

func TestHandoff_CreatesNote(t *testing.T) {
	svc := newTestService(t, "hand")
	ctx := context.Background()

	art, err := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{
		Labels:   []string{"kind:effort.task", "scope:hand", "work.active", "priority:low"},
		Title:    "Hand task",
		Sections: []parchment.Section{{Name: "context", Text: "ctx"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	claim := service.Find("claim")
	_, _ = claim.Run(ctx, svc, mustJSON(map[string]any{"id": art.ID, "agent": "a1"}))

	handoff := service.Find("handoff")
	out, err := handoff.Run(ctx, svc, mustJSON(map[string]any{
		"artifact_id":  art.ID,
		"from_session": "s1",
		"to_session":   "s2",
		"agent":        "a1",
		"to_agent":     "a2",
		"summary":      "passing baton",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "handoff note") {
		t.Fatalf("unexpected: %s", out)
	}
}

func mustJSON(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}
