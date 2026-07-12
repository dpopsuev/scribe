package service_test

import (
	"context"
	"encoding/json"
	"testing"

	parchment "github.com/dpopsuev/parchment"
	"github.com/dpopsuev/scribe/service"
)

func TestStampLastMutation_PreservesProvenance(t *testing.T) {
	svc := newTestService(t, "prov")
	svc.SessionID = "ses-test-1"
	svc.ClientHarness = "cursor"
	ctx := context.Background()

	created, err := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{
		Labels:   []string{"kind:effort.task", "scope:prov", "work.draft", "priority:medium"},
		Title:    "Mutation Target",
		Sections: []parchment.Section{{Name: "context", Text: "x"}},
		Extra:    service.StampProvenance(nil, service.AgentProvenance("cursor", "1", "ses-create")),
	})
	if err != nil {
		t.Fatal(err)
	}

	op := service.Find("set")
	raw, _ := json.Marshal(map[string]any{"id": created.ID, "field": "title", "value": "Mutation Target Updated"})
	if _, err := op.Run(ctx, svc, raw); err != nil {
		t.Fatal(err)
	}

	got, err := svc.Proto.GetArtifact(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	prov, _ := got.Extra["provenance"].(map[string]any)
	if prov == nil || prov["session_id"] != "ses-create" {
		t.Fatalf("create provenance clobbered: %#v", got.Extra["provenance"])
	}
	mut, _ := got.Extra["last_mutation"].(map[string]any)
	if mut == nil || mut["action"] != "set" || mut["session_id"] != "ses-test-1" {
		t.Fatalf("last_mutation missing/wrong: %#v", got.Extra["last_mutation"])
	}
	if mut["harness"] != "cursor" {
		t.Fatalf("harness=%v", mut["harness"])
	}
}
