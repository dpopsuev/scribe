package mcp_test

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"

	parchment "github.com/dpopsuev/parchment"
	scribemcp "github.com/dpopsuev/scribe/mcp"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestAttach_StoresAndReturnsImage(t *testing.T) {
	// Given an artifact exists
	s := openStore(t)
	ctx := context.Background()
	_ = s.Put(ctx, &parchment.Artifact{
		ID: "ART-ATTACH-1", Title: "Attach test", Labels: []string{"kind:intent.spec", "work.draft", "project:test"},
	})

	srv, _ := scribemcp.NewServerFromStore(s, []string{"test"}, parchment.ProtocolConfig{}, "test")
	cs := connectClient(t, srv)

	// When: attach a base64-encoded PNG
	pngMagic := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}
	encoded := base64.StdEncoding.EncodeToString(pngMagic)
	out := callTool(t, cs, "artifact", map[string]any{
		"action":       "attach",
		"id":           "ART-ATTACH-1",
		"name":         "diagram.png",
		"content_type": "image/png",
		"data":         encoded,
	})

	if !strings.Contains(out, "attached") || !strings.Contains(out, "diagram.png") {
		t.Errorf("unexpected attach response: %s", out)
	}

	// When: get the artifact — should return mixed content
	result := callToolRaw(t, cs, "artifact", map[string]any{
		"action": "get",
		"id":     "ART-ATTACH-1",
	})

	var hasText, hasImage bool
	for _, c := range result.Content {
		switch c.(type) {
		case *sdkmcp.TextContent:
			hasText = true
		case *sdkmcp.ImageContent:
			hasImage = true
		}
	}
	if !hasText {
		t.Error("get should include a TextContent block")
	}
	if !hasImage {
		t.Error("get should include an ImageContent block for the attached PNG")
	}
}

func TestDetach_RemovesAttachment(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()
	_ = s.Put(ctx, &parchment.Artifact{
		ID: "ART-DETACH-1", Title: "Detach test", Labels: []string{"kind:intent.spec", "work.draft", "project:test"},
	})
	_ = s.PutAttachment(ctx, "ART-DETACH-1", "img.png", "image/png", []byte{1, 2, 3})

	srv, _ := scribemcp.NewServerFromStore(s, []string{"test"}, parchment.ProtocolConfig{}, "test")
	cs := connectClient(t, srv)

	out := callTool(t, cs, "artifact", map[string]any{
		"action": "detach",
		"id":     "ART-DETACH-1",
		"name":   "img.png",
	})
	if !strings.Contains(out, "detached") {
		t.Errorf("unexpected detach response: %s", out)
	}

	// get should now return only TextContent
	result := callToolRaw(t, cs, "artifact", map[string]any{
		"action": "get",
		"id":     "ART-DETACH-1",
	})
	for _, c := range result.Content {
		if _, ok := c.(*sdkmcp.ImageContent); ok {
			t.Error("no ImageContent expected after detach")
		}
	}
}

// callToolRaw returns the full CallToolResult instead of just the text.
func callToolRaw(t *testing.T, cs *sdkmcp.ClientSession, name string, args map[string]any) *sdkmcp.CallToolResult {
	t.Helper()
	result, err := cs.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name:      name,
		Arguments: args,
	})
	if err != nil {
		t.Fatalf("CallTool %s: %v", name, err)
	}
	return result
}
