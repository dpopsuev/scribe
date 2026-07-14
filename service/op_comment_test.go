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

func TestCommentAddList_DiscussesOrdered(t *testing.T) {
	svc := newTestService(t, "mesh")
	ctx := context.Background()

	task, err := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{
		Labels:   []string{"kind:effort.task", parchment.LabelPrefixScope + "mesh", "work.draft"},
		Title:    "Task under discussion",
		Sections: []parchment.Section{{Name: "context", Text: "ctx"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	add := service.Find("comment_add")
	if add == nil {
		t.Fatal("comment_add not registered")
	}
	for _, text := range []string{"first comment", "second comment"} {
		raw, _ := json.Marshal(map[string]any{"id": task.ID, "text": text, "author": "alice"})
		if _, err := add.Run(ctx, svc, raw); err != nil {
			t.Fatalf("comment_add %q: %v", text, err)
		}
		time.Sleep(2 * time.Millisecond)
	}

	edges, err := svc.Proto.Store().Neighbors(ctx, task.ID, parchment.RelDiscusses, parchment.Incoming)
	if err != nil {
		t.Fatal(err)
	}
	if len(edges) != 2 {
		t.Fatalf("incoming discusses = %d, want 2", len(edges))
	}

	list := service.Find("comment_list")
	out, err := list.Run(ctx, svc, mustJSON(map[string]any{"id": task.ID}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "first comment") || !strings.Contains(out, "second comment") {
		t.Fatalf("list missing bodies: %s", out)
	}
	i1 := strings.Index(out, "first comment")
	i2 := strings.Index(out, "second comment")
	if i1 < 0 || i2 < 0 || i1 > i2 {
		t.Fatalf("expected oldest-first order, got: %s", out)
	}
}

func TestCommentAdd_RequiresTarget(t *testing.T) {
	svc := newTestService(t, "mesh")
	add := service.Find("comment_add")
	_, err := add.Run(context.Background(), svc, mustJSON(map[string]any{
		"id": "missing-id", "text": "nope",
	}))
	if err == nil {
		t.Fatal("expected error for missing target")
	}
}
