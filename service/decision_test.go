package service_test

import (
	"context"
	"strings"
	"testing"

	parchment "github.com/dpopsuev/parchment"
	"github.com/dpopsuev/scribe/service"
)

func TestDecision_Record_ThenCheck_ReturnsAnswer(t *testing.T) {
	// Given: no decision recorded yet
	// When: RecordDecision then CheckDecision
	// Then: CheckDecision returns the recorded answer
	t.Parallel()
	svc := newTestService(t)
	ctx := context.Background()

	if err := svc.RecordDecision(ctx, "testing-framework", "stdlib only — no testify", "test"); err != nil {
		t.Fatalf("RecordDecision: %v", err)
	}
	result, err := svc.CheckDecision(ctx, "testing-framework", "test")
	if err != nil {
		t.Fatalf("CheckDecision: %v", err)
	}
	if result == "" {
		t.Error("CheckDecision should return the recorded decision")
	}
	if !strings.Contains(result, "stdlib only") {
		t.Errorf("CheckDecision = %q, want to contain 'stdlib only'", result)
	}
}

func TestDecision_CheckUndecided_ReturnsEmpty(t *testing.T) {
	// Given: no decision recorded for key
	// When: CheckDecision
	// Then: returns empty string (not an error)
	t.Parallel()
	svc := newTestService(t)
	result, err := svc.CheckDecision(context.Background(), "nonexistent-key", "test")
	if err != nil {
		t.Fatalf("CheckDecision on unknown key should not error: %v", err)
	}
	if result != "" {
		t.Errorf("expected empty for undecided key, got: %q", result)
	}
}

func TestDecision_List_ReturnsAll(t *testing.T) {
	// Given: two decisions recorded
	// When: ListDecisions
	// Then: both appear
	t.Parallel()
	store := parchment.NewMemoryStore()
	proto := parchment.New(store, parchment.KnowledgeSchema(), []string{"test"}, nil, parchment.ProtocolConfig{})
	svc := service.New(proto, nil, []string{"test"})
	ctx := context.Background()

	_ = svc.RecordDecision(ctx, "linter", "golangci-lint", "test")
	_ = svc.RecordDecision(ctx, "formatter", "gofmt", "test")

	arts, err := svc.ListDecisions(ctx, "test")
	if err != nil {
		t.Fatal(err)
	}
	if len(arts) < 2 {
		t.Errorf("expected >= 2 decisions, got %d", len(arts))
	}
}
