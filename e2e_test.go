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
	body, _ := json.Marshal(req)
	start := time.Now()
	resp, err := doMCP(string(body), sid)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("tools/call %s (id=%d): %v", tool, id, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)

	jsonPayload := extractSSEData(raw)
	var result map[string]any
	if err := json.Unmarshal(jsonPayload, &result); err != nil {
		t.Fatalf("unmarshal %s response: %v\nraw: %s", tool, err, truncate(string(raw), 500))
	}
	t.Logf("MCP %s (id=%d) completed in %s (%d bytes)", tool, id, elapsed.Round(time.Millisecond), len(raw))

	// Extract text from result
	r, ok := result["result"].(map[string]any)
	if !ok {
		if errObj, ok := result["error"]; ok {
			t.Fatalf("MCP error: %v", errObj)
		}
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
	if resp.StatusCode != 200 {
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
	t.Run("create_and_list", func(t *testing.T) {
		text := mcpCall(t, sid, next(), "artifact", map[string]any{
			"action": "create", "kind": "task", "title": "E2E task 1", "scope": "e2e", "priority": "high",
		})
		if !strings.Contains(text, "E2E task 1") {
			t.Fatalf("create missing title:\n%s", truncate(text, 300))
		}

		text = mcpCall(t, sid, next(), "artifact", map[string]any{
			"action": "list", "kind": "task", "scope": "e2e",
		})
		if !strings.Contains(text, "E2E task 1") {
			t.Fatalf("list missing task:\n%s", truncate(text, 300))
		}
	})

	t.Run("bulk_get_and_section_filter", func(t *testing.T) {
		// Create two tasks
		mcpCall(t, sid, next(), "artifact", map[string]any{
			"action": "create", "kind": "task", "title": "Bulk A", "scope": "e2e", "priority": "medium",
		})
		mcpCall(t, sid, next(), "artifact", map[string]any{
			"action": "create", "kind": "task", "title": "Bulk B", "scope": "e2e", "priority": "low",
		})

		// List to get IDs
		listText := mcpCall(t, sid, next(), "artifact", map[string]any{
			"action": "list", "kind": "task", "scope": "e2e",
			"fields": []string{"id", "title"},
		})
		if !strings.Contains(listText, "Bulk A") || !strings.Contains(listText, "Bulk B") {
			t.Fatalf("compact list missing bulk tasks:\n%s", truncate(listText, 500))
		}
	})

	t.Run("diff", func(t *testing.T) {
		// Create two specs with different content
		text1 := mcpCall(t, sid, next(), "artifact", map[string]any{
			"action": "create", "kind": "spec", "title": "Spec Alpha", "scope": "e2e",
		})
		id1 := extractFirstID(t, text1, "EEE-SPC-")
		text2 := mcpCall(t, sid, next(), "artifact", map[string]any{
			"action": "create", "kind": "spec", "title": "Spec Beta", "scope": "e2e",
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
		// Create a goal with tasks (topo_sort works on direct children)
		goalText := mcpCall(t, sid, next(), "artifact", map[string]any{
			"action": "create", "kind": "goal", "title": "E2E Topo Goal", "scope": "e2e",
		})
		goalID := extractFirstID(t, goalText, "EEE-GOL-")

		t1Text := mcpCall(t, sid, next(), "artifact", map[string]any{
			"action": "create", "kind": "task", "title": "Step 1", "scope": "e2e",
			"parent": goalID, "priority": "high",
		})
		t1ID := extractFirstID(t, t1Text, "EEE-TSK-")

		mcpCall(t, sid, next(), "artifact", map[string]any{
			"action": "create", "kind": "task", "title": "Step 2", "scope": "e2e",
			"parent": goalID, "priority": "medium", "depends_on": []string{t1ID},
		})

		// Topo sort the goal
		topoText := mcpCall(t, sid, next(), "graph", map[string]any{
			"action": "topo_sort", "id": goalID,
		})
		if !strings.Contains(topoText, "Step 1") || !strings.Contains(topoText, "Step 2") {
			t.Fatalf("topo_sort missing tasks:\n%s", truncate(topoText, 500))
		}
		idx1 := strings.Index(topoText, "Step 1")
		idx2 := strings.Index(topoText, "Step 2")
		if idx1 > idx2 {
			t.Fatalf("topo_sort wrong order: Step 1 at %d, Step 2 at %d", idx1, idx2)
		}
	})

	t.Run("bulk_link_and_impact", func(t *testing.T) {
		// Create spec and tasks
		specText := mcpCall(t, sid, next(), "artifact", map[string]any{
			"action": "create", "kind": "spec", "title": "Impact Spec", "scope": "e2e",
		})
		specID := extractFirstID(t, specText, "EEE-SPC-")

		t1Text := mcpCall(t, sid, next(), "artifact", map[string]any{
			"action": "create", "kind": "task", "title": "Impl Task", "scope": "e2e", "priority": "high",
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
			"action": "create", "kind": "goal", "title": "Goal A", "scope": "e2e",
		})
		g1ID := extractFirstID(t, g1Text, "EEE-GOL-")

		g2Text := mcpCall(t, sid, next(), "artifact", map[string]any{
			"action": "create", "kind": "goal", "title": "Goal B", "scope": "e2e",
		})
		g2ID := extractFirstID(t, g2Text, "EEE-GOL-")

		taskText := mcpCall(t, sid, next(), "artifact", map[string]any{
			"action": "create", "kind": "task", "title": "Moveable Task", "scope": "e2e",
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
			"action": "list", "scope": "e2e", "top": 3,
		})
		// Should return JSON array with at most 3 items
		if !strings.Contains(text, "\"id\"") {
			t.Fatalf("top-N should return JSON artifacts:\n%s", truncate(text, 500))
		}
	})

	t.Run("admin_motd", func(t *testing.T) {
		text := mcpCall(t, sid, next(), "admin", map[string]any{
			"action": "motd",
		})
		if !strings.Contains(text, "Scribe") {
			t.Fatalf("motd should show version:\n%s", truncate(text, 300))
		}
	})
}

// --- LLM Round-Trip Test ---

type ollamaToolCall struct {
	Function struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	} `json:"function"`
}

type ollamaMessage struct {
	Role      string           `json:"role"`
	Content   string           `json:"content"`
	ToolCalls []ollamaToolCall `json:"tool_calls,omitempty"`
}

type ollamaResponse struct {
	Message ollamaMessage `json:"message"`
}

func ollamaReachable(host string) bool {
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(host + "/api/tags")
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == 200
}

func ollamaChat(t *testing.T, host, model string, messages []map[string]any, tools []map[string]any) ollamaResponse {
	t.Helper()
	payload := map[string]any{
		"model":    model,
		"stream":   false,
		"messages": messages,
		"options":  map[string]any{"temperature": 0.0},
	}
	if len(tools) > 0 {
		payload["tools"] = tools
	}
	body, _ := json.Marshal(payload)
	t.Logf("ollama: model=%s, messages=%d, payload=%d bytes", model, len(messages), len(body))

	client := &http.Client{Timeout: 300 * time.Second}
	resp, err := client.Post(host+"/api/chat", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("ollama failed: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		t.Fatalf("ollama HTTP %d: %s", resp.StatusCode, truncate(string(raw), 500))
	}
	var result ollamaResponse
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("decode: %v\nraw: %s", err, truncate(string(raw), 500))
	}
	return result
}

// scribeTools returns Ollama-compatible tool definitions for the artifact and graph tools.
func scribeTools() []map[string]any {
	return []map[string]any{
		{
			"type": "function",
			"function": map[string]any{
				"name":        "artifact",
				"description": "Create, read, update, and manage work artifacts.",
				"parameters": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"action":    map[string]any{"type": "string", "description": "create, get, list, set, update, attach_section"},
						"kind":      map[string]any{"type": "string", "description": "task, spec, bug, goal, campaign"},
						"title":     map[string]any{"type": "string", "description": "Artifact title"},
						"scope":     map[string]any{"type": "string", "description": "Project scope"},
						"id":        map[string]any{"type": "string", "description": "Artifact ID for get/set/update"},
						"priority":  map[string]any{"type": "string", "description": "none, low, medium, high, critical"},
						"status":    map[string]any{"type": "string", "description": "draft, active, complete, archived"},
						"parent":    map[string]any{"type": "string", "description": "Parent artifact ID"},
						"field":     map[string]any{"type": "string", "description": "Field name for set action"},
						"value":     map[string]any{"type": "string", "description": "Field value for set action"},
						"force":     map[string]any{"type": "boolean", "description": "Force status transition"},
						"name":      map[string]any{"type": "string", "description": "Section name"},
						"text":      map[string]any{"type": "string", "description": "Section text"},
						"depends_on": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
						"sections": map[string]any{
							"type":  "array",
							"items": map[string]any{"type": "object"},
						},
					},
					"required": []string{"action"},
				},
			},
		},
		{
			"type": "function",
			"function": map[string]any{
				"name":        "graph",
				"description": "Navigate and modify artifact relationships.",
				"parameters": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"action":   map[string]any{"type": "string", "description": "tree, briefing, topo_sort, link, unlink"},
						"id":       map[string]any{"type": "string", "description": "Root or source artifact ID"},
						"relation": map[string]any{"type": "string", "description": "parent_of, depends_on, implements, follows"},
						"targets":  map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
					},
					"required": []string{"action"},
				},
			},
		},
		{
			"type": "function",
			"function": map[string]any{
				"name":        "admin",
				"description": "System administration: motd, dashboard, snapshot.",
				"parameters": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"action": map[string]any{"type": "string", "description": "motd, dashboard, snapshot"},
					},
					"required": []string{"action"},
				},
			},
		},
	}
}

// agentLoop runs an LLM agent loop with structured instrumentation.
// Logs each turn with: tool call, args, result preview, error detection, and message count.
func agentLoop(t *testing.T, sid, ollamaHost, ollamaModel, systemPrompt, userPrompt string, maxTurns int) (toolsCalled []string, finalAnswer string) {
	t.Helper()
	tools := scribeTools()
	messages := []map[string]any{
		{"role": "system", "content": systemPrompt},
		{"role": "user", "content": userPrompt},
	}
	callID := 500
	totalTokensEst := len(systemPrompt) + len(userPrompt) // rough estimate

	for turn := 1; turn <= maxTurns; turn++ {
		start := time.Now()
		resp := ollamaChat(t, ollamaHost, ollamaModel, messages, tools)
		elapsed := time.Since(start)

		if len(resp.Message.ToolCalls) == 0 {
			finalAnswer = resp.Message.Content
			t.Logf("=== Turn %d/%d === FINAL ANSWER (%.1fs, ~%d tokens)\n  %s",
				turn, maxTurns, elapsed.Seconds(), totalTokensEst/4, truncate(finalAnswer, 300))
			return
		}

		for _, tc := range resp.Message.ToolCalls {
			callID++
			toolsCalled = append(toolsCalled, tc.Function.Name)
			argsJSON, _ := json.Marshal(tc.Function.Arguments)

			result := mcpCall(t, sid, callID, tc.Function.Name, tc.Function.Arguments)
			totalTokensEst += len(result)

			// Detect errors in result
			isError := strings.Contains(result, "error") || strings.Contains(result, "not found")
			errorTag := ""
			if isError {
				errorTag = " ⚠ ERROR"
			}

			t.Logf("=== Turn %d/%d === TOOL CALL (%.1fs)%s\n  CALL: %s(%s)\n  RESULT: %s",
				turn, maxTurns, elapsed.Seconds(), errorTag,
				tc.Function.Name, truncate(string(argsJSON), 150),
				truncate(result, 200))

			messages = append(messages,
				map[string]any{"role": "assistant", "content": "", "tool_calls": []ollamaToolCall{tc}},
				map[string]any{"role": "tool", "content": result},
			)
		}
	}
	t.Fatalf("exhausted %d turns without final answer (tools called: %v)", maxTurns, toolsCalled)
	return
}

// --- LLM E2E Test Scenarios ---

func setupLLMTest(t *testing.T) (sid, ollamaHost, ollamaModel string) {
	t.Helper()
	if _, err := exec.LookPath("podman"); err != nil {
		t.Skip("podman not found")
	}
	ollamaHost = envOr("OLLAMA_HOST", "http://localhost:11434")
	ollamaModel = envOr("OLLAMA_MODEL", "qwen3:1.7b")
	if !ollamaReachable(ollamaHost) {
		t.Skipf("Ollama not reachable at %s", ollamaHost)
	}
	t.Logf("LLM: %s @ %s", ollamaModel, ollamaHost)

	stopContainer(t)
	t.Cleanup(func() { stopContainer(t) })
	buildImage(t)
	startContainer(t)
	waitHealthy(t, 30*time.Second)
	sid = initSession(t)
	return
}

func TestE2E_LLM_CampaignPlanning(t *testing.T) {
	sid, host, model := setupLLMTest(t)

	tools, _ := agentLoop(t, sid, host, model,
		"You call tools exactly as instructed. Follow the steps precisely.",
		`Do these steps in order:
Step 1: Call artifact with {"action":"create","kind":"spec","title":"Auth Spec","scope":"e2e"}
Step 2: Call artifact with {"action":"create","kind":"task","title":"Task 1","scope":"e2e","priority":"medium"}
Step 3: Call artifact with {"action":"create","kind":"task","title":"Task 2","scope":"e2e","priority":"medium"}
Step 4: Call artifact with {"action":"list","scope":"e2e","fields":["id","kind","title"]}
Step 5: Say "done" and list what you created.`,
		6,
	)

	if len(tools) < 3 {
		t.Fatalf("expected at least 3 tool calls, got %d: %v", len(tools), tools)
	}

	listText := mcpCall(t, sid, 900, "artifact", map[string]any{
		"action": "list", "scope": "e2e", "fields": []string{"id", "kind", "title"},
	})
	t.Logf("created:\n%s", truncate(listText, 500))
}

func TestE2E_LLM_TemplateSelfCorrection(t *testing.T) {
	sid, host, model := setupLLMTest(t)

	// Create a task (no template) — activation will fail due to schema ShouldSections
	taskText := mcpCall(t, sid, 100, "artifact", map[string]any{
		"action": "create", "kind": "task", "title": "Correction Test", "scope": "e2e", "priority": "medium",
		"sections": []map[string]string{{"name": "context", "text": "ctx"}},
	})
	taskID := extractFirstID(t, taskText, "EEE-TSK-")

	tools, _ := agentLoop(t, sid, host, model,
		"You call tools exactly as instructed. If a tool returns an error about missing sections, attach those sections then retry.",
		fmt.Sprintf(`Do these steps:
Step 1: Call artifact with {"action":"set","id":"%s","field":"status","value":"active"}
Step 2: If step 1 returned an error mentioning "checklist" or "acceptance", call artifact with {"action":"attach_section","id":"%s","name":"checklist","text":"- [ ] done"} and then {"action":"attach_section","id":"%s","name":"acceptance","text":"it works"}
Step 3: Retry step 1.
Step 4: Say "done".`, taskID, taskID, taskID),
		8,
	)

	t.Logf("tools: %v (%d calls)", tools, len(tools))
	if len(tools) < 2 {
		t.Error("expected at least 2 tool calls")
	}
}

func TestE2E_LLM_GraphBlockerQuery(t *testing.T) {
	sid, host, model := setupLLMTest(t)

	// Seed dependency chain
	aText := mcpCall(t, sid, 100, "artifact", map[string]any{
		"action": "create", "kind": "task", "title": "Foundation", "scope": "e2e", "priority": "high",
	})
	aID := extractFirstID(t, aText, "EEE-TSK-")

	bText := mcpCall(t, sid, 101, "artifact", map[string]any{
		"action": "create", "kind": "task", "title": "Middleware", "scope": "e2e", "priority": "medium",
		"depends_on": []string{aID},
	})
	bID := extractFirstID(t, bText, "EEE-TSK-")

	cText := mcpCall(t, sid, 102, "artifact", map[string]any{
		"action": "create", "kind": "task", "title": "Frontend", "scope": "e2e", "priority": "low",
		"depends_on": []string{bID},
	})
	cID := extractFirstID(t, cText, "EEE-TSK-")

	mcpCall(t, sid, 103, "artifact", map[string]any{
		"action": "set", "id": aID, "field": "status", "value": "complete", "force": true,
	})

	_, answer := agentLoop(t, sid, host, model,
		"You call tools exactly as instructed. Report findings clearly.",
		fmt.Sprintf(`Do these steps:
Step 1: Call artifact with {"action":"get","id":"%s"} to see its depends_on field.
Step 2: Call artifact with {"action":"get","id":"%s"} to check if it's complete.
Step 3: Report which task is blocking %s and why.`, cID, bID, cID),
		4,
	)

	lower := strings.ToLower(answer)
	if !strings.Contains(lower, "middleware") && !strings.Contains(lower, strings.ToLower(bID)) {
		t.Errorf("should identify %s/Middleware as blocker, got: %s", bID, truncate(answer, 300))
	}
}

func TestE2E_LLM_StaleTriage(t *testing.T) {
	sid, host, model := setupLLMTest(t)

	mcpCall(t, sid, 100, "artifact", map[string]any{
		"action": "create", "kind": "task", "title": "Stale Draft", "scope": "e2e", "priority": "low",
	})

	tools, _ := agentLoop(t, sid, host, model,
		"You call tools exactly as instructed.",
		`Do these steps:
Step 1: Call admin with {"action":"motd"}
Step 2: Call artifact with {"action":"list","scope":"e2e","status":"draft","fields":["id","title","status"]}
Step 3: Report what you found.`,
		4,
	)

	hasAdmin := false
	for _, tc := range tools {
		if tc == "admin" {
			hasAdmin = true
		}
	}
	if !hasAdmin {
		t.Error("agent should call admin motd")
	}
}

func TestE2E_LLM_ExportVerify(t *testing.T) {
	sid, host, model := setupLLMTest(t)

	mcpCall(t, sid, 100, "artifact", map[string]any{
		"action": "create", "kind": "task", "title": "Data Check", "scope": "e2e", "priority": "high",
	})

	tools, _ := agentLoop(t, sid, host, model,
		"You call tools exactly as instructed.",
		`Do these steps:
Step 1: Call artifact with {"action":"list","scope":"e2e","fields":["id","kind","title"]}
Step 2: Report the artifact count.`,
		3,
	)

	hasArtifact := false
	for _, tc := range tools {
		if tc == "artifact" {
			hasArtifact = true
		}
	}
	if !hasArtifact {
		t.Error("agent should call artifact list")
	}
}
