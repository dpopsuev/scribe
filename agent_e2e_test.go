//go:build agent_e2e

package agent_e2e_test

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

type canarySDK struct {
	name       string
	enabledEnv string
	apiKeyEnv  string
	apiKeyHint string
	sdkPkgPath string
	sdkMissMsg string
	titleFmt   string
	scope      string
	npmScript  string
	envPrefix  string
}

var sdks = []canarySDK{
	{
		name:       "Claude",
		enabledEnv: "RUN_CLAUDE_E2E",
		apiKeyEnv:  "ANTHROPIC_API_KEY",
		apiKeyHint: "ANTHROPIC_API_KEY not set",
		sdkPkgPath: "agent_e2e/node_modules/@anthropic-ai/claude-agent-sdk/package.json",
		sdkMissMsg: "Claude Agent SDK not installed; run `make claude-e2e-setup` first",
		titleFmt:   "Claude Agent SDK canary %d",
		scope:      "claude-e2e",
		npmScript:  "claude-canary",
		envPrefix:  "CLAUDE_CANARY",
	},
	{
		name:       "Cursor",
		enabledEnv: "RUN_CURSOR_E2E",
		apiKeyEnv:  "CURSOR_API_KEY",
		apiKeyHint: "CURSOR_API_KEY not set",
		sdkPkgPath: "agent_e2e/node_modules/@cursor/sdk/package.json",
		sdkMissMsg: "Cursor SDK not installed; run `make cursor-e2e-setup` first",
		titleFmt:   "Cursor SDK canary %d",
		scope:      "cursor-e2e",
		npmScript:  "cursor-canary",
		envPrefix:  "CURSOR_CANARY",
	},
}

func TestAgentSDKCanary(t *testing.T) {
	requireCmd(t, "git")
	requireCmd(t, "node")
	requireCmd(t, "npm")

	repoRoot := findRepoRoot(t)

	for _, sdk := range sdks {
		sdk := sdk
		t.Run(sdk.name, func(t *testing.T) {
			if os.Getenv(sdk.enabledEnv) == "" {
				t.Skipf("set %s=1 to enable the %s canary", sdk.enabledEnv, sdk.name)
			}
			if os.Getenv(sdk.apiKeyEnv) == "" {
				t.Skip(sdk.apiKeyHint)
			}
			if _, err := os.Stat(filepath.Join(repoRoot, sdk.sdkPkgPath)); err != nil {
				t.Skip(sdk.sdkMissMsg)
			}

			workdir := createWorkspace(t)
			baseURL, logs := startScribeServer(t, repoRoot)

			title := fmt.Sprintf(sdk.titleFmt, time.Now().UnixNano())
			runCanary(t, sdk, repoRoot, baseURL, workdir, title, logs)

			listText := artifactList(t, baseURL, sdk.scope, title)
			if !strings.Contains(listText, title) {
				t.Fatalf("expected artifact %q in Scribe list output, got:\n%s", title, listText)
			}
		})
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
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find repo root (no go.mod)")
		}
		dir = parent
	}
}

func createWorkspace(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# Agent SDK canary\n"), 0o644); err != nil {
		t.Fatalf("create workspace README: %v", err)
	}
	return dir
}

func startScribeServer(t *testing.T, repoRoot string) (string, *bytes.Buffer) {
	t.Helper()
	scribeBin := filepath.Join(repoRoot, "bin", "scribe")
	if _, err := os.Stat(scribeBin); err != nil {
		scribeBin = filepath.Join(repoRoot, "scribe")
	}
	if _, err := os.Stat(scribeBin); err != nil {
		t.Skip("scribe binary not found; run `make build` first")
	}

	dbPath := filepath.Join(t.TempDir(), "canary.sqlite")
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("find free port: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()

	var logs bytes.Buffer
	cmd := exec.Command(scribeBin, "serve", "--db", dbPath, "--addr", fmt.Sprintf("127.0.0.1:%d", port))
	cmd.Stdout = &logs
	cmd.Stderr = &logs
	if err := cmd.Start(); err != nil {
		t.Fatalf("start scribe: %v", err)
	}
	t.Cleanup(func() { cmd.Process.Kill() })

	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(baseURL + "/health")
		if err == nil && resp.StatusCode == http.StatusOK {
			resp.Body.Close()
			return baseURL, &logs
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("scribe server did not become healthy\nlogs:\n%s", logs.String())
	return "", nil
}

func runCanary(t *testing.T, sdk canarySDK, repoRoot, baseURL, workdir, title string, logs *bytes.Buffer) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, "npm", "run", "--silent", sdk.npmScript)
	cmd.Dir = filepath.Join(repoRoot, "agent_e2e")
	cmd.Env = append(os.Environ(),
		sdk.envPrefix+"_SCRIBE_URL="+baseURL,
		sdk.envPrefix+"_WORKDIR="+workdir,
		sdk.envPrefix+"_TITLE="+title,
		sdk.envPrefix+"_SCOPE="+sdk.scope,
		sdk.envPrefix+"_PRIORITY=medium",
	)

	out, err := cmd.CombinedOutput()
	if len(out) > 0 {
		t.Logf("%s canary output:\n%s", sdk.name, string(out))
	}
	if ctx.Err() == context.DeadlineExceeded {
		t.Fatalf("%s canary timed out\noutput:\n%s\nscribe logs:\n%s", sdk.name, string(out), logs.String())
	}
	if err != nil {
		t.Fatalf("%s canary failed: %v\noutput:\n%s\nscribe logs:\n%s", sdk.name, err, string(out), logs.String())
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
	result, _ := body["result"].(map[string]any)
	content, _ := result["content"].([]any)
	if len(content) == 0 {
		return ""
	}
	first, _ := content[0].(map[string]any)
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
			"clientInfo":      map[string]any{"name": "agent-e2e-test", "version": "0.1"},
		},
	})
	sid := resp.Header.Get("Mcp-Session-Id")
	resp.Body.Close()
	if sid == "" {
		t.Fatal("initialize response missing Mcp-Session-Id")
	}
	notif := postJSONRPC(t, baseURL, sid, map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"})
	notif.Body.Close()
	return sid
}

func postJSONRPC(t *testing.T, baseURL, sid string, payload map[string]any) *http.Response {
	t.Helper()
	body, _ := json.Marshal(payload)
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
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("POST %s: HTTP %d: %s", req.URL.String(), resp.StatusCode, string(raw))
	}
	return resp
}

func decodeJSONRPC(t *testing.T, resp *http.Response) map[string]any {
	t.Helper()
	raw, _ := io.ReadAll(resp.Body)
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
		t.Fatalf("decode JSON-RPC: %v\nraw: %s", err, string(raw))
	}
	if errObj, ok := payload["error"]; ok {
		data, _ := json.Marshal(errObj)
		t.Fatalf("JSON-RPC error: %s", string(data))
	}
	return payload
}
