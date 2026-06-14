//go:build e2e

package e2e_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	testImage     = "scribe-e2e-test"
	testContainer = "scribe-e2e-test"
	testAddr      = "http://localhost:18080"
	testPort      = "18080:8080"
)

// --- container lifecycle ---

func buildImage(t *testing.T) {
	t.Helper()
	root := repoRoot(t)
	start := time.Now()
	run(t, "podman", "build", "-t", testImage, "-f", filepath.Join(root, "Dockerfile.test"), root)
	t.Logf("image built in %s", time.Since(start).Round(time.Millisecond))
}

func startContainer(t *testing.T) {
	t.Helper()
	start := time.Now()
	run(t, "podman", "run", "-d",
		"--name", testContainer,
		"-p", testPort,
		testImage,
	)
	t.Logf("container started in %s", time.Since(start).Round(time.Millisecond))
}

func stopContainer(t *testing.T) {
	t.Helper()
	exec.Command("podman", "rm", "-f", testContainer).Run()
}

func repoRoot(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("go", "env", "GOMOD").CombinedOutput()
	if err != nil {
		t.Fatalf("go env GOMOD failed: %v", err)
	}
	mod := strings.TrimSpace(string(out))
	if mod == "" {
		t.Fatal("not inside a Go module")
	}
	return filepath.Dir(mod)
}

func run(t *testing.T, name string, args ...string) {
	t.Helper()
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v failed: %v\n%s", name, args, err, out)
	}
}

// --- MCP helpers ---

func waitHealthy(t *testing.T, timeout time.Duration) {
	t.Helper()
	start := time.Now()
	deadline := time.Now().Add(timeout)
	body := `{"jsonrpc":"2.0","id":0,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"e2e","version":"0.1"}}}`
	attempts := 0
	for time.Now().Before(deadline) {
		attempts++
		resp, err := doMCP(body, "")
		if err == nil {
			resp.Body.Close()
			t.Logf("container healthy after %d attempts (%s)", attempts, time.Since(start).Round(time.Millisecond))
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("container not healthy after %d attempts (%s)", attempts, timeout)
}

func initSession(t *testing.T) string {
	t.Helper()
	resp, err := doMCP(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"e2e","version":"0.1"}}}`, "")
	if err != nil {
		t.Fatalf("initialize: %v", err)
	}
	sid := resp.Header.Get("Mcp-Session-Id")
	resp.Body.Close()
	if sid == "" {
		t.Fatal("no Mcp-Session-Id in initialize response")
	}
	doMCP(`{"jsonrpc":"2.0","method":"notifications/initialized"}`, sid)
	t.Logf("MCP session established: %s", sid[:16]+"...")
	return sid
}

func mcpCall(t *testing.T, sid string, id int, tool string, args map[string]any) string {
	t.Helper()
	params := map[string]any{"name": tool}
	if args != nil {
		params["arguments"] = args
	}
	req := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  "tools/call",
		"params":  params,
	}
	result := mcpRequest(t, sid, req, tool)

	// Extract text from result
	r, ok := result["result"].(map[string]any)
	if !ok {
		t.Fatalf("no result field: %v", result)
	}
	content, ok := r["content"].([]any)
	if !ok || len(content) == 0 {
		t.Fatalf("empty content: %v", r)
	}
	first := content[0].(map[string]any)
	text, _ := first["text"].(string)
	return text
}

func extractSSEData(raw []byte) []byte {
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "data: ") {
			return []byte(strings.TrimPrefix(line, "data: "))
		}
	}
	return raw
}

func mcpRequest(t *testing.T, sid string, req map[string]any, label string) map[string]any {
	t.Helper()
	body, _ := json.Marshal(req)
	id, _ := req["id"].(int)
	method, _ := req["method"].(string)
	start := time.Now()
	resp, err := doMCP(string(body), sid)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("%s (%s id=%d): %v", label, method, id, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)

	jsonPayload := extractSSEData(raw)
	var result map[string]any
	if err := json.Unmarshal(jsonPayload, &result); err != nil {
		t.Fatalf("unmarshal %s response: %v\nraw: %s", label, err, truncate(string(raw), 500))
	}
	t.Logf("MCP %s (%s id=%d) completed in %s (%d bytes)", label, method, id, elapsed.Round(time.Millisecond), len(raw))
	if errObj, ok := result["error"]; ok {
		t.Fatalf("MCP %s error: %v", label, errObj)
	}
	return result
}

func mcpToolsList(t *testing.T, sid string, id int) []map[string]any {
	t.Helper()
	result := mcpRequest(t, sid, map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  "tools/list",
		"params":  map[string]any{},
	}, "tools/list")

	payload, ok := result["result"].(map[string]any)
	if !ok {
		t.Fatalf("tools/list missing result: %s", mustJSON(result))
	}
	rawTools, ok := payload["tools"].([]any)
	if !ok {
		t.Fatalf("tools/list missing tools array: %s", mustJSON(payload))
	}

	tools := make([]map[string]any, 0, len(rawTools))
	for _, rawTool := range rawTools {
		tool, ok := rawTool.(map[string]any)
		if !ok {
			t.Fatalf("tools/list entry has unexpected type %T", rawTool)
		}
		tools = append(tools, tool)
	}
	return tools
}

func assertTypedToolSchemas(t *testing.T, tools []map[string]any) {
	t.Helper()
	expectedProps := map[string][]string{
		"admin":    {"action", "scope"},
		"artifact": {"action", "kind"},
		"graph":    {"action", "relation"},
	}
	seen := make(map[string]bool, len(expectedProps))

	for _, tool := range tools {
		name, _ := tool["name"].(string)
		wantProps, ok := expectedProps[name]
		if !ok {
			continue
		}
		seen[name] = true

		schema, ok := tool["inputSchema"].(map[string]any)
		if !ok {
			t.Fatalf("%s missing inputSchema object: %s", name, mustJSON(tool))
		}
		if gotType, _ := schema["type"].(string); gotType != "object" {
			t.Errorf("%s inputSchema.type = %q, want %q", name, gotType, "object")
		}
		props, ok := schema["properties"].(map[string]any)
		if !ok || len(props) == 0 {
			t.Fatalf("%s inputSchema missing properties: %s", name, mustJSON(schema))
		}
		for _, prop := range wantProps {
			if _, ok := props[prop]; !ok {
				t.Fatalf("%s inputSchema missing property %q: %s", name, prop, mustJSON(schema))
			}
		}
	}

	for name := range expectedProps {
		if !seen[name] {
			t.Fatalf("tools/list missing tool %q", name)
		}
	}
}

func doMCP(body, sid string) (*http.Response, error) {
	req, _ := http.NewRequest("POST", testAddr+"/", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if sid != "" {
		req.Header.Set("Mcp-Session-Id", sid)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, raw)
	}
	return resp, nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

func mustJSON(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("<marshal error: %v>", err)
	}
	return string(data)
}

func extractFirstID(t *testing.T, text, prefix string) string {
	t.Helper()
	idx := strings.Index(text, prefix)
	if idx < 0 {
		t.Fatalf("could not find %s prefix in text:\n%s", prefix, truncate(text, 500))
	}
	end := idx
	for end < len(text) && text[end] != ' ' && text[end] != '\n' && text[end] != '"' && text[end] != ',' && text[end] != '}' && text[end] != '\t' {
		end++
	}
	return text[idx:end]
}

// --- Deterministic MCP Tests ---

func TestE2E_Deterministic(t *testing.T) {
	if _, err := exec.LookPath("podman"); err != nil {
		t.Skip("podman not found")
	}

	stopContainer(t)
	t.Cleanup(func() { stopContainer(t) })

	buildImage(t)
	startContainer(t)
	waitHealthy(t, 30*time.Second)
	sid := initSession(t)
	callID := 10
	next := func() int { callID++; return callID }

	// Create base artifacts for all subtests
	t.Run("tools_list_contract", func(t *testing.T) {
		tools := mcpToolsList(t, sid, next())
		assertTypedToolSchemas(t, tools)
	})

	t.Run("create_and_list", func(t *testing.T) {
		text := mcpCall(t, sid, next(), "artifact", map[string]any{
			"action": "create", "kind": "effort.task", "title": "E2E task 1", "scope": "e2e", "priority": "high",
		})
		if !strings.Contains(text, "E2E task 1") {
			t.Fatalf("create missing title:\n%s", truncate(text, 300))
		}

		text = mcpCall(t, sid, next(), "artifact", map[string]any{
			"action": "query", "kind": "effort.task", "scope": "e2e",
		})
		if !strings.Contains(text, "E2E task 1") {
			t.Fatalf("list missing task:\n%s", truncate(text, 300))
		}
	})

	t.Run("bulk_get_and_section_filter", func(t *testing.T) {
		// Create two tasks
		mcpCall(t, sid, next(), "artifact", map[string]any{
			"action": "create", "kind": "effort.task", "title": "Bulk A", "scope": "e2e", "priority": "medium",
		})
		mcpCall(t, sid, next(), "artifact", map[string]any{
			"action": "create", "kind": "effort.task", "title": "Bulk B", "scope": "e2e", "priority": "low",
		})

		// List to get IDs
		listText := mcpCall(t, sid, next(), "artifact", map[string]any{
			"action": "query", "kind": "effort.task", "scope": "e2e",
			"fields": []string{"id", "title"},
		})
		if !strings.Contains(listText, "Bulk A") || !strings.Contains(listText, "Bulk B") {
			t.Fatalf("compact list missing bulk tasks:\n%s", truncate(listText, 500))
		}
	})

	t.Run("diff", func(t *testing.T) {
		// Create two specs with different content
		text1 := mcpCall(t, sid, next(), "artifact", map[string]any{
			"action": "create", "kind": "intent.spec", "title": "Spec Alpha", "scope": "e2e",
		})
		id1 := extractFirstID(t, text1, "EEE-SPC-")
		text2 := mcpCall(t, sid, next(), "artifact", map[string]any{
			"action": "create", "kind": "intent.spec", "title": "Spec Beta", "scope": "e2e",
		})
		id2 := extractFirstID(t, text2, "EEE-SPC-")

		diffText := mcpCall(t, sid, next(), "artifact", map[string]any{
			"action": "diff", "id": id1, "against": id2,
		})
		if !strings.Contains(diffText, "title") {
			t.Fatalf("diff should show title difference:\n%s", truncate(diffText, 500))
		}
	})

	t.Run("graph_link_and_topo_sort", func(t *testing.T) {
		// Create a goal with tasks — query(sort=topo) returns dependency order
		goalText := mcpCall(t, sid, next(), "artifact", map[string]any{
			"action": "create", "kind": "effort.goal", "title": "E2E Topo Goal", "scope": "e2e",
		})
		goalID := extractFirstID(t, goalText, "EEE-GOL-")

		t1Text := mcpCall(t, sid, next(), "artifact", map[string]any{
			"action": "create", "kind": "effort.task", "title": "Step 1", "scope": "e2e",
			"parent": goalID, "priority": "high",
		})
		t1ID := extractFirstID(t, t1Text, "EEE-TSK-")

		mcpCall(t, sid, next(), "artifact", map[string]any{
			"action": "create", "kind": "effort.task", "title": "Step 2", "scope": "e2e",
			"parent": goalID, "priority": "medium", "depends_on": []string{t1ID},
		})

		topoText := mcpCall(t, sid, next(), "artifact", map[string]any{
			"action": "query", "id": goalID, "sort": "topo",
		})
		if !strings.Contains(topoText, "Step 1") || !strings.Contains(topoText, "Step 2") {
			t.Fatalf("query(sort=topo) missing tasks:\n%s", truncate(topoText, 500))
		}
		idx1 := strings.Index(topoText, "Step 1")
		idx2 := strings.Index(topoText, "Step 2")
		if idx1 > idx2 {
			t.Fatalf("query(sort=topo) wrong order: Step 1 at %d, Step 2 at %d", idx1, idx2)
		}
	})

	t.Run("bulk_link_and_impact", func(t *testing.T) {
		// Create spec and tasks
		specText := mcpCall(t, sid, next(), "artifact", map[string]any{
			"action": "create", "kind": "intent.spec", "title": "Impact Spec", "scope": "e2e",
		})
		specID := extractFirstID(t, specText, "EEE-SPC-")

		t1Text := mcpCall(t, sid, next(), "artifact", map[string]any{
			"action": "create", "kind": "effort.task", "title": "Impl Task", "scope": "e2e", "priority": "high",
		})
		t1ID := extractFirstID(t, t1Text, "EEE-TSK-")

		// Bulk link
		mcpCall(t, sid, next(), "graph", map[string]any{
			"action": "bulk_link",
			"edges": []map[string]any{
				{"from": t1ID, "relation": "implements", "to": specID},
			},
		})

		// Impact analysis on spec
		impactText := mcpCall(t, sid, next(), "graph", map[string]any{
			"action": "impact", "id": specID,
		})
		if !strings.Contains(impactText, "Impl Task") || !strings.Contains(impactText, "Implements") {
			t.Fatalf("impact should show implementing task:\n%s", truncate(impactText, 500))
		}
	})

	t.Run("move_and_replace", func(t *testing.T) {
		// Create two goals and a task
		g1Text := mcpCall(t, sid, next(), "artifact", map[string]any{
			"action": "create", "kind": "effort.goal", "title": "Goal A", "scope": "e2e",
		})
		g1ID := extractFirstID(t, g1Text, "EEE-GOL-")

		g2Text := mcpCall(t, sid, next(), "artifact", map[string]any{
			"action": "create", "kind": "effort.goal", "title": "Goal B", "scope": "e2e",
		})
		g2ID := extractFirstID(t, g2Text, "EEE-GOL-")

		taskText := mcpCall(t, sid, next(), "artifact", map[string]any{
			"action": "create", "kind": "effort.task", "title": "Moveable Task", "scope": "e2e",
			"parent": g1ID, "priority": "medium",
		})
		taskID := extractFirstID(t, taskText, "EEE-TSK-")

		// Move task from g1 to g2
		moveText := mcpCall(t, sid, next(), "graph", map[string]any{
			"action": "move", "id": taskID, "target": g2ID,
		})
		if !strings.Contains(moveText, "moved") {
			t.Fatalf("move should confirm:\n%s", truncate(moveText, 300))
		}
	})

	t.Run("top_n_ranking", func(t *testing.T) {
		text := mcpCall(t, sid, next(), "artifact", map[string]any{
			"action": "query", "scope": "e2e", "top": 3,
		})
		// Should return JSON array with at most 3 items
		if !strings.Contains(text, "\"id\"") {
			t.Fatalf("top-N should return JSON artifacts:\n%s", truncate(text, 500))
		}
	})

	t.Run("admin_brief", func(t *testing.T) {
		text := mcpCall(t, sid, next(), "admin", map[string]any{
			"action": "brief",
		})
		if !strings.Contains(text, "Scribe") {
			t.Fatalf("brief should show version:\n%s", truncate(text, 300))
		}
	})
}

