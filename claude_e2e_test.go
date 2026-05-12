//go:build claude_e2e

package claude_e2e_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestClaudeAgentSDKCanary(t *testing.T) {
	if os.Getenv("RUN_CLAUDE_E2E") == "" {
		t.Skip("set RUN_CLAUDE_E2E=1 to enable the Claude Agent SDK canary")
	}
	if os.Getenv("ANTHROPIC_API_KEY") == "" {
		t.Skip("ANTHROPIC_API_KEY not set")
	}

	requireCmd(t, "git")
	requireCmd(t, "node")
	requireCmd(t, "npm")

	repoRoot := findRepoRoot(t)
	if _, err := os.Stat(filepath.Join(repoRoot, "agent_e2e", "node_modules", "@anthropic-ai", "claude-agent-sdk", "package.json")); err != nil {
		t.Skip("Claude Agent SDK not installed; run `make claude-e2e-setup` first")
	}

	workdir := createWorkspace(t)
	baseURL, logs := startScribeServer(t, repoRoot)

	title := fmt.Sprintf("Claude Agent SDK canary %d", time.Now().UnixNano())
	scope := "claude-e2e"

	runClaudeCanary(t, repoRoot, baseURL, workdir, title, scope, logs)

	listText := artifactList(t, baseURL, scope, title)
	if !strings.Contains(listText, title) {
		t.Fatalf("expected created artifact %q in Scribe list output, got:\n%s", title, listText)
	}
}

func requireCmd(t *testing.T, name string) {
	t.Helper()
	if _, err := exec.LookPath(name); err != nil {
		t.Skipf("%s not found", name)
	}
}

func findRepoRoot(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("go", "env", "GOMOD").CombinedOutput()
	if err != nil {
		t.Fatalf("go env GOMOD failed: %v", err)
	}
	mod := strings.TrimSpace(string(out))
	if mod == "" || mod == os.DevNull {
		t.Fatal("not inside a Go module")
	}
	return filepath.Dir(mod)
}

func createWorkspace(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# Claude Agent SDK canary\n"), 0o644); err != nil {
		t.Fatalf("write workspace README: %v", err)
	}
	cmd := exec.Command("git", "init", "-q")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	return dir
}

func startScribeServer(t *testing.T, repoRoot string) (string, *bytes.Buffer) {
	t.Helper()
	addr := freeLocalAddr(t)
	dbPath := filepath.Join(t.TempDir(), "claude-e2e.db")
	binary := buildScribeBinary(t, repoRoot)

	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, binary, "--db", dbPath, "serve", "--transport", "http", "--addr", addr)
	cmd.Dir = repoRoot

	var logs bytes.Buffer
	cmd.Stdout = &logs
	cmd.Stderr = &logs

	if err := cmd.Start(); err != nil {
		t.Fatalf("start scribe server: %v", err)
	}

	t.Cleanup(func() {
		cancel()
		_ = cmd.Wait()
		if t.Failed() {
			t.Logf("scribe server logs:\n%s", logs.String())
		}
	})

	waitForVersion(t, "http://"+addr, 30*time.Second, &logs)
	return "http://" + addr + "/", &logs
}

func buildScribeBinary(t *testing.T, repoRoot string) string {
	t.Helper()
	outPath := filepath.Join(t.TempDir(), "scribe-e2e-binary")
	cmd := exec.Command("go", "build", "-o", outPath, "./cmd/scribe")
	cmd.Dir = repoRoot
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build scribe binary: %v\n%s", err, out)
	}
	return outPath
}

func freeLocalAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve local port: %v", err)
	}
	defer l.Close()
	return l.Addr().String()
}

func waitForVersion(t *testing.T, baseURL string, timeout time.Duration, logs *bytes.Buffer) {
	t.Helper()
	client := &http.Client{Timeout: time.Second}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := client.Get(baseURL + "/version")
		if err == nil && resp.StatusCode == http.StatusOK {
			resp.Body.Close()
			return
		}
		if resp != nil {
			resp.Body.Close()
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("scribe server did not become healthy within %s\nlogs:\n%s", timeout, logs.String())
}

func runClaudeCanary(t *testing.T, repoRoot, baseURL, workdir, title, scope string, logs *bytes.Buffer) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, "npm", "run", "--silent", "claude-canary")
	cmd.Dir = filepath.Join(repoRoot, "agent_e2e")
	cmd.Env = append(os.Environ(),
		"CLAUDE_CANARY_SCRIBE_URL="+baseURL,
		"CLAUDE_CANARY_WORKDIR="+workdir,
		"CLAUDE_CANARY_TITLE="+title,
		"CLAUDE_CANARY_SCOPE="+scope,
		"CLAUDE_CANARY_PRIORITY=medium",
	)

	out, err := cmd.CombinedOutput()
	if len(out) > 0 {
		t.Logf("claude canary output:\n%s", string(out))
	}
	if ctx.Err() == context.DeadlineExceeded {
		t.Fatalf("claude canary timed out after %s\noutput:\n%s\nscribe logs:\n%s", 5*time.Minute, string(out), logs.String())
	}
	if err != nil {
		t.Fatalf("claude canary failed: %v\noutput:\n%s\nscribe logs:\n%s", err, string(out), logs.String())
	}
}

func artifactList(t *testing.T, baseURL, scope, title string) string {
	t.Helper()
	sid := initializeMCP(t, baseURL)
	payload := map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "artifact",
			"arguments": map[string]any{
				"action": "list",
				"scope":  scope,
				"query":  title,
				"fields": []string{"id", "title", "kind"},
			},
		},
	}

	resp := postJSONRPC(t, baseURL, sid, payload)
	defer resp.Body.Close()
	body := decodeJSONRPC(t, resp)
	result, ok := body["result"].(map[string]any)
	if !ok {
		t.Fatalf("tools/call artifact missing result: %s", mustJSON(body))
	}
	content, ok := result["content"].([]any)
	if !ok || len(content) == 0 {
		t.Fatalf("tools/call artifact missing content: %s", mustJSON(result))
	}
	first, ok := content[0].(map[string]any)
	if !ok {
		t.Fatalf("unexpected content payload: %s", mustJSON(content[0]))
	}
	text, _ := first["text"].(string)
	return text
}

func initializeMCP(t *testing.T, baseURL string) string {
	t.Helper()
	resp := postJSONRPC(t, baseURL, "", map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]any{},
			"clientInfo": map[string]any{
				"name":    "claude-e2e-test",
				"version": "0.1",
			},
		},
	})
	sid := resp.Header.Get("Mcp-Session-Id")
	resp.Body.Close()
	if sid == "" {
		t.Fatal("initialize response missing Mcp-Session-Id")
	}

	initResp := postJSONRPC(t, baseURL, sid, map[string]any{
		"jsonrpc": "2.0",
		"method":  "notifications/initialized",
	})
	initResp.Body.Close()
	return sid
}

func postJSONRPC(t *testing.T, baseURL, sid string, payload map[string]any) *http.Response {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal JSON-RPC payload: %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, strings.TrimRight(baseURL, "/")+"/", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("build JSON-RPC request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if sid != "" {
		req.Header.Set("Mcp-Session-Id", sid)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", req.URL.String(), err)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("POST %s: HTTP %d: %s", req.URL.String(), resp.StatusCode, string(raw))
	}
	return resp
}

func decodeJSONRPC(t *testing.T, resp *http.Response) map[string]any {
	t.Helper()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read JSON-RPC response: %v", err)
	}

	body := raw
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "data: ") {
			body = []byte(strings.TrimPrefix(line, "data: "))
			break
		}
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode JSON-RPC response: %v\nraw: %s", err, string(raw))
	}
	if errObj, ok := payload["error"]; ok {
		t.Fatalf("JSON-RPC returned error: %s", mustJSON(errObj))
	}
	return payload
}

func mustJSON(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("<marshal error: %v>", err)
	}
	return string(data)
}
