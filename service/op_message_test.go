package service_test

import (
	"context"
	"strings"
	"testing"
	"time"

	parchment "github.com/dpopsuev/parchment"
	"github.com/dpopsuev/scribe/service"
)

func TestMessage_AddListUnderParent(t *testing.T) {
	svc := newTestService(t, "mesh")
	ctx := context.Background()
	parent, err := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{
		Labels:   []string{"kind:knowledge.context", parchment.LabelPrefixScope + "mesh"},
		Title:    "Thread container",
		Sections: []parchment.Section{{Name: "content", Text: "t"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	add := service.Find("message_add")
	list := service.Find("message_list")
	if _, err := add.Run(ctx, svc, mustJSON(map[string]any{
		"parent": parent.ID, "text": "hello", "author": "alice", "scope": "mesh",
	})); err != nil {
		t.Fatal(err)
	}
	out, err := list.Run(ctx, svc, mustJSON(map[string]any{
		"id": parent.ID, "mode": "children",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "hello") {
		t.Fatalf("list: %s", out)
	}
}

func TestMessage_WikilinkMentions(t *testing.T) {
	svc := newTestService(t, "mesh")
	ctx := context.Background()
	task, err := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{
		Labels:   []string{"kind:effort.task", parchment.LabelPrefixScope + "mesh", "work.draft"},
		Title:    "Linked task",
		Sections: []parchment.Section{{Name: "context", Text: "c"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	parent, err := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{
		Labels:   []string{"kind:knowledge.context", parchment.LabelPrefixScope + "mesh"},
		Title:    "Container",
		Sections: []parchment.Section{{Name: "content", Text: "c"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	add := service.Find("message_add")
	if _, err := add.Run(ctx, svc, mustJSON(map[string]any{
		"parent": parent.ID, "scope": "mesh",
		"text": "blocking on [[" + task.ID + "]]",
	})); err != nil {
		t.Fatal(err)
	}
	children, err := svc.Proto.Children(ctx, parent.ID)
	if err != nil || len(children) == 0 {
		t.Fatalf("children: %v", err)
	}
	edges, _ := svc.Proto.Store().Neighbors(ctx, children[0].ID, parchment.RelMentions, parchment.Outgoing)
	found := false
	for _, e := range edges {
		if e.To == task.ID {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected mentions to %s, got %+v", task.ID, edges)
	}
	if _, err := add.Run(ctx, svc, mustJSON(map[string]any{
		"parent": parent.ID, "text": "[[missing-xyz-nope]]",
	})); err != nil {
		t.Fatalf("unknown wikilink must not fail: %v", err)
	}
}

func TestMessage_CursorUnread(t *testing.T) {
	svc := newTestService(t, "mesh")
	ctx := context.Background()
	parent, err := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{
		Labels:   []string{"kind:knowledge.context", parchment.LabelPrefixScope + "mesh"},
		Title:    "Cursored",
		Sections: []parchment.Section{{Name: "content", Text: "c"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	add := service.Find("message_add")
	list := service.Find("message_list")
	mark := service.Find("cursor_mark")
	if _, err := add.Run(ctx, svc, mustJSON(map[string]any{
		"parent": parent.ID, "text": "old", "scope": "mesh",
	})); err != nil {
		t.Fatal(err)
	}
	time.Sleep(5 * time.Millisecond)
	if _, err := mark.Run(ctx, svc, mustJSON(map[string]any{
		"session": "s1", "key": parent.ID,
	})); err != nil {
		t.Fatal(err)
	}
	time.Sleep(5 * time.Millisecond)
	if _, err := add.Run(ctx, svc, mustJSON(map[string]any{
		"parent": parent.ID, "text": "new", "scope": "mesh",
	})); err != nil {
		t.Fatal(err)
	}
	out, err := list.Run(ctx, svc, mustJSON(map[string]any{
		"id": parent.ID, "mode": "children", "session": "s1",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "old") {
		t.Fatalf("cursor should filter old: %s", out)
	}
	if !strings.Contains(out, "new") {
		t.Fatalf("expected new: %s", out)
	}
}
