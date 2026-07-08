package mcp_test

import (
	"context"
	"strings"
	"testing"

	parchment "github.com/dpopsuev/parchment"
	scribemcp "github.com/dpopsuev/scribe/mcp"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMCP_KernelCreate(t *testing.T) {
	s := openStore(t)
	srv, _ := scribemcp.NewServerFromStore(s, []string{"test"}, parchment.ProtocolConfig{}, "test")
	cs := connectClient(t, srv)

	result := callTool(t, cs, "artifact", map[string]any{
		"action":  "kernel_create",
		"title":   "Extracted PTP insight",
		"content": "The offset threshold is 100ns for T-BC.",
		"scope":   "test",
	})
	if !strings.Contains(result, "created kernel") {
		t.Fatalf("unexpected result: %s", result)
	}
	if !strings.Contains(result, "kernel.pending") {
		t.Fatalf("expected kernel.pending in result: %s", result)
	}
}

func TestMCP_KernelCreateWithPointer(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()

	// Seed a pointer artifact
	_ = s.Put(ctx, &parchment.Artifact{
		ID:     "SRC-MCP-1",
		Title:  "Source doc",
		Labels: []string{"kind:knowledge.source"},
	})

	srv, _ := scribemcp.NewServerFromStore(s, []string{"test"}, parchment.ProtocolConfig{}, "test")
	cs := connectClient(t, srv)

	result := callTool(t, cs, "artifact", map[string]any{
		"action":     "kernel_create",
		"id":         "KRN-MCP-1",
		"title":      "Linked kernel",
		"content":    "Kernel content from source.",
		"pointer_id": "SRC-MCP-1",
		"section":    "body",
		"line":       float64(10),
	})
	if !strings.Contains(result, "KRN-MCP-1") {
		t.Fatalf("expected KRN-MCP-1 in result: %s", result)
	}

	// Verify the edge was created
	edges, _ := s.Neighbors(ctx, "KRN-MCP-1", "traces_to", parchment.Outgoing)
	if len(edges) != 1 || edges[0].To != "SRC-MCP-1" {
		t.Errorf("expected traces_to edge to SRC-MCP-1, got %v", edges)
	}
}

func TestMCP_KernelConfirm(t *testing.T) {
	s := openStore(t)
	srv, _ := scribemcp.NewServerFromStore(s, []string{"test"}, parchment.ProtocolConfig{}, "test")
	cs := connectClient(t, srv)

	// Create a kernel first
	callTool(t, cs, "artifact", map[string]any{
		"action":  "kernel_create",
		"id":      "KRN-CONF-1",
		"title":   "To confirm",
		"content": "Content.",
	})

	// Confirm it
	result := callTool(t, cs, "artifact", map[string]any{
		"action": "kernel_confirm",
		"id":     "KRN-CONF-1",
	})
	if !strings.Contains(result, "confirmed") {
		t.Fatalf("expected 'confirmed' in result: %s", result)
	}

	// Verify status
	art, _ := s.Get(context.Background(), "KRN-CONF-1")
	status := parchment.StatusFromLabels(art.Labels)
	if status != "kernel.confirmed" {
		t.Errorf("status = %q, want kernel.confirmed", status)
	}
}

func TestMCP_KernelReject(t *testing.T) {
	s := openStore(t)
	srv, _ := scribemcp.NewServerFromStore(s, []string{"test"}, parchment.ProtocolConfig{}, "test")
	cs := connectClient(t, srv)

	callTool(t, cs, "artifact", map[string]any{
		"action":  "kernel_create",
		"id":      "KRN-REJ-1",
		"title":   "To reject",
		"content": "Bad content.",
	})

	result := callTool(t, cs, "artifact", map[string]any{
		"action": "kernel_reject",
		"id":     "KRN-REJ-1",
	})
	if !strings.Contains(result, "rejected") {
		t.Fatalf("expected 'rejected' in result: %s", result)
	}

	art, _ := s.Get(context.Background(), "KRN-REJ-1")
	status := parchment.StatusFromLabels(art.Labels)
	if status != "kernel.rejected" {
		t.Errorf("status = %q, want kernel.rejected", status)
	}
}

func TestMCP_KernelConfirmMissingID(t *testing.T) {
	s := openStore(t)
	srv, _ := scribemcp.NewServerFromStore(s, []string{"test"}, parchment.ProtocolConfig{}, "test")
	cs := connectClient(t, srv)

	result, err := cs.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name:      "artifact",
		Arguments: map[string]any{"action": "kernel_confirm"},
	})
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected tool error for missing id")
	}
}

func TestMCP_KernelCreateMissingTitle(t *testing.T) {
	s := openStore(t)
	srv, _ := scribemcp.NewServerFromStore(s, []string{"test"}, parchment.ProtocolConfig{}, "test")
	cs := connectClient(t, srv)

	result, err := cs.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name:      "artifact",
		Arguments: map[string]any{"action": "kernel_create", "content": "stuff"},
	})
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected tool error for missing title")
	}
}
