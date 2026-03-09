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

func mcpToolCall(t *testing.T, sid string, id int, tool string, args map[string]any) map[string]any {
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
	t.Logf("MCP %s (id=%d) completed in %s (%d bytes)",
		tool, id, elapsed.Round(time.Millisecond), len(raw))
	return result
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

func extractText(t *testing.T, result map[string]any) string {
	t.Helper()
	r, ok := result["result"].(map[string]any)
	if !ok {
		if errObj, ok := result["error"]; ok {
			t.Fatalf("MCP error: %v", errObj)
		}
		t.Fatalf("no result field in response: %v", result)
	}
	content, ok := r["content"].([]any)
	if !ok || len(content) == 0 {
		t.Fatalf("empty content in result: %v", r)
	}
	first := content[0].(map[string]any)
	text, _ := first["text"].(string)
	return text
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

// --- Deterministic MCP Tool Tests ---

func TestE2E_Deterministic(t *testing.T) {
	if _, err := exec.LookPath("podman"); err != nil {
		t.Skip("podman not found")
	}

	stopContainer(t)
	t.Cleanup(func() { stopContainer(t) })

	t.Log("=== Phase 1: Deterministic MCP Tool Tests ===")
	buildImage(t)
	startContainer(t)
	waitHealthy(t, 30*time.Second)

	sid := initSession(t)
	callID := 10
	nextID := func() int { callID++; return callID }

	t.Run("create_artifact", func(t *testing.T) {
		text := extractText(t, mcpToolCall(t, sid, nextID(), "create_artifact", map[string]any{
			"kind":  "contract",
			"title": "E2E test contract",
			"scope": "e2e",
		}))
		if !strings.Contains(text, "E2E test contract") {
			t.Fatalf("create response missing title:\n%s", truncate(text, 500))
		}
		if !strings.Contains(text, "CON-") {
			t.Fatalf("create response missing CON- prefix:\n%s", truncate(text, 500))
		}
		t.Logf("created: %s", truncate(text, 200))
	})

	t.Run("list_artifacts", func(t *testing.T) {
		text := extractText(t, mcpToolCall(t, sid, nextID(), "list_artifacts", map[string]any{
			"kind": "contract",
		}))
		if !strings.Contains(text, "E2E test contract") {
			t.Fatalf("list missing created contract:\n%s", truncate(text, 500))
		}
	})

	t.Run("search_artifacts", func(t *testing.T) {
		text := extractText(t, mcpToolCall(t, sid, nextID(), "search_artifacts", map[string]any{
			"query": "E2E test",
		}))
		if !strings.Contains(text, "E2E test contract") {
			t.Fatalf("search missing contract:\n%s", truncate(text, 500))
		}
	})

	t.Run("attach_and_get_section", func(t *testing.T) {
		text := extractText(t, mcpToolCall(t, sid, nextID(), "list_artifacts", map[string]any{
			"kind": "contract",
		}))
		var id string
		for _, line := range strings.Split(text, "\n") {
			if strings.Contains(line, "CON-") {
				parts := strings.Fields(line)
				for _, p := range parts {
					if strings.HasPrefix(p, "CON-") {
						id = p
						break
					}
				}
				if id != "" {
					break
				}
			}
		}
		if id == "" {
			id = extractFirstID(t, text, "CON-")
		}

		attachText := extractText(t, mcpToolCall(t, sid, nextID(), "attach_section", map[string]any{
			"id":   id,
			"name": "design",
			"text": "## Architecture\n\nThis is the **design** section.",
		}))
		t.Logf("attach result: %s", truncate(attachText, 200))

		getSecText := extractText(t, mcpToolCall(t, sid, nextID(), "get_section", map[string]any{
			"id":   id,
			"name": "design",
		}))
		if !strings.Contains(getSecText, "Architecture") {
			t.Fatalf("get_section missing content:\n%s", truncate(getSecText, 500))
		}
	})

	t.Run("inventory", func(t *testing.T) {
		text := extractText(t, mcpToolCall(t, sid, nextID(), "inventory", nil))
		if !strings.Contains(text, "contract") {
			t.Fatalf("inventory missing contract kind:\n%s", truncate(text, 500))
		}
	})
}

func extractFirstID(t *testing.T, text, prefix string) string {
	t.Helper()
	idx := strings.Index(text, prefix)
	if idx < 0 {
		t.Fatalf("could not find %s prefix in text:\n%s", prefix, truncate(text, 500))
	}
	end := idx
	for end < len(text) && text[end] != ' ' && text[end] != '\n' && text[end] != '"' && text[end] != ',' && text[end] != '}' {
		end++
	}
	return text[idx:end]
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

func ollamaChatWithTools(t *testing.T, host, model string, messages []map[string]any, tools []map[string]any) ollamaResponse {
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

func TestE2E_LLMRoundTrip(t *testing.T) {
	if _, err := exec.LookPath("podman"); err != nil {
		t.Skip("podman not found")
	}

	ollamaHost := envOr("OLLAMA_HOST", "http://localhost:11434")
	ollamaModel := envOr("OLLAMA_MODEL", "qwen2.5:32b")

	t.Logf("=== Phase 2: LLM Round-Trip (model=%s) ===", ollamaModel)

	if !ollamaReachable(ollamaHost) {
		t.Skipf("Ollama not reachable at %s — skipping", ollamaHost)
	}

	stopContainer(t)
	t.Cleanup(func() { stopContainer(t) })

	buildImage(t)
	startContainer(t)
	waitHealthy(t, 30*time.Second)
	sid := initSession(t)

	scribeTools := []map[string]any{
		{
			"type": "function",
			"function": map[string]any{
				"name":        "create_artifact",
				"description": "Create a new governance artifact. You MUST call this to create contracts.",
				"parameters": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"kind":  map[string]any{"type": "string", "description": "Artifact kind (contract, sprint, goal)"},
						"title": map[string]any{"type": "string", "description": "Artifact title"},
						"scope": map[string]any{"type": "string", "description": "Project scope"},
						"goal":  map[string]any{"type": "string", "description": "Goal description"},
					},
					"required": []string{"kind", "title"},
				},
			},
		},
		{
			"type": "function",
			"function": map[string]any{
				"name":        "list_artifacts",
				"description": "List governance artifacts with optional filters.",
				"parameters": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"kind":   map[string]any{"type": "string", "description": "Filter by kind"},
						"scope":  map[string]any{"type": "string", "description": "Filter by scope"},
						"status": map[string]any{"type": "string", "description": "Filter by status"},
					},
				},
			},
		},
	}

	messages := []map[string]any{
		{"role": "system", "content": "You are a project manager. Use the tools provided to manage governance artifacts. Always create artifacts when asked."},
		{"role": "user", "content": "Create a contract titled 'LLM Integration Test' in scope 'e2e' with goal 'Verify LLM can call Scribe MCP tools'. Then list all contracts to confirm it was created."},
	}

	toolCalled := false
	createCalled := false
	listCalled := false
	maxTurns := 6

	for turn := 1; turn <= maxTurns; turn++ {
		t.Logf("--- Turn %d/%d ---", turn, maxTurns)
		start := time.Now()
		resp := ollamaChatWithTools(t, ollamaHost, ollamaModel, messages, scribeTools)
		t.Logf("responded in %s", time.Since(start).Round(time.Millisecond))

		if len(resp.Message.ToolCalls) > 0 {
			tc := resp.Message.ToolCalls[0]
			argsJSON, _ := json.Marshal(tc.Function.Arguments)
			t.Logf("tool call: %s(%s)", tc.Function.Name, string(argsJSON))
			toolCalled = true

			if tc.Function.Name == "create_artifact" {
				createCalled = true
			}
			if tc.Function.Name == "list_artifacts" {
				listCalled = true
			}

			toolResult := extractText(t, mcpToolCall(t, sid, 300+turn, tc.Function.Name, tc.Function.Arguments))
			t.Logf("tool result: %d bytes", len(toolResult))

			messages = append(messages,
				map[string]any{"role": "assistant", "content": "", "tool_calls": resp.Message.ToolCalls},
				map[string]any{"role": "tool", "content": toolResult},
			)
			continue
		}

		answer := resp.Message.Content
		t.Logf("answer (%d chars): %s", len(answer), truncate(answer, 500))

		if !toolCalled {
			t.Fatal("LLM answered WITHOUT calling any tool — agent loop broken")
		}
		if !createCalled {
			t.Fatal("LLM never called create_artifact")
		}
		t.Logf("PASS: create_artifact called=%v, list_artifacts called=%v", createCalled, listCalled)
		return
	}

	if !toolCalled {
		t.Fatal("exhausted turns without tool call")
	}
	t.Fatal("exhausted turns without final answer")
}
