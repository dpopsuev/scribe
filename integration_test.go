//go:build integration

package integration_test

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
	image     = "scribe-integration-test"
	container = "scribe-integration-test"
	addr      = "http://localhost:18080"
	port      = "18080:8080"
	volume    = "scribe-integration-test-data"
)

func TestContainerLifecycle(t *testing.T) {
	requireCmd(t, "podman")
	cleanup(t)
	t.Cleanup(func() { cleanup(t) })

	// go test may change cwd; find the repo root via the go.mod file
	repoRoot := findRepoRoot(t)
	t.Logf("repo root: %s", repoRoot)

	t.Log("Building image from Dockerfile...")
	run(t, "podman", "build", "-t", image, repoRoot)

	t.Log("Starting container...")
	run(t, "podman", "run", "-d", "--name", container, "-p", port, "-v", volume+":/data", image)
	waitHealthy(t, 10*time.Second)

	sid := initialize(t)

	t.Run("motd returns without error", func(t *testing.T) {
		resp := callTool(t, sid, "motd", map[string]any{}, 2)
		if !strings.Contains(resp, "nothing to report") && !strings.Contains(resp, "Goal") {
			t.Fatalf("unexpected motd response: %s", resp)
		}
	})

	var artifactID string
	t.Run("create artifact", func(t *testing.T) {
		resp := callTool(t, sid, "create_artifact", map[string]any{
			"kind":  "task",
			"title": "integration-test-artifact",
		}, 3)
		// response contains the artifact JSON with an ID
		if !strings.Contains(resp, "integration-test-artifact") {
			t.Fatalf("create_artifact didn't return the title: %s", resp)
		}
		var art struct{ ID string }
		if err := json.Unmarshal([]byte(resp), &art); err == nil && art.ID != "" {
			artifactID = art.ID
		}
		if artifactID == "" {
			t.Fatalf("could not extract artifact ID from: %s", resp)
		}
		t.Logf("created: %s", artifactID)
	})

	t.Run("get artifact", func(t *testing.T) {
		resp := callTool(t, sid, "get_artifact", map[string]any{"id": artifactID}, 4)
		if !strings.Contains(resp, "integration-test-artifact") {
			t.Fatalf("get_artifact didn't return the artifact: %s", resp)
		}
	})

	t.Run("persistence across restart", func(t *testing.T) {
		t.Log("Stopping container...")
		run(t, "podman", "stop", container)
		run(t, "podman", "rm", container)

		t.Log("Starting fresh container with same volume...")
		run(t, "podman", "run", "-d", "--name", container, "-p", port, "-v", volume+":/data", image)
		waitHealthy(t, 10*time.Second)

		sid2 := initialize(t)
		resp := callTool(t, sid2, "get_artifact", map[string]any{"id": artifactID}, 2)
		if !strings.Contains(resp, "integration-test-artifact") {
			t.Fatalf("artifact lost after restart: %s", resp)
		}
		t.Logf("artifact %s survived restart", artifactID)
	})
}

// --- helpers ---

func findRepoRoot(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("go", "env", "GOMOD").CombinedOutput()
	if err != nil {
		t.Fatalf("go env GOMOD: %v", err)
	}
	mod := strings.TrimSpace(string(out))
	if mod == "" || mod == os.DevNull {
		t.Fatal("not inside a Go module")
	}
	return filepath.Dir(mod)
}

func requireCmd(t *testing.T, name string) {
	t.Helper()
	if _, err := exec.LookPath(name); err != nil {
		t.Skipf("%s not found, skipping integration test", name)
	}
}

func run(t *testing.T, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v failed: %v\n%s", name, args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func cleanup(t *testing.T) {
	t.Helper()
	exec.Command("podman", "rm", "-f", container).Run()
	exec.Command("podman", "volume", "rm", "-f", volume).Run()
	exec.Command("podman", "rmi", "-f", image).Run()
}

func waitHealthy(t *testing.T, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	body := `{"jsonrpc":"2.0","id":0,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"healthcheck","version":"0.1"}}}`
	for time.Now().Before(deadline) {
		resp, err := doMCP(body, "")
		if err == nil {
			resp.Body.Close()
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatal("container did not become healthy within timeout")
}

func initialize(t *testing.T) string {
	t.Helper()
	body := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"0.1"}}}`
	resp, err := doMCP(body, "")
	if err != nil {
		t.Fatalf("initialize: %v", err)
	}
	sid := resp.Header.Get("Mcp-Session-Id")
	if sid == "" {
		t.Fatal("no Mcp-Session-Id in initialize response")
	}

	// send initialized notification
	_, _ = doMCP(`{"jsonrpc":"2.0","method":"notifications/initialized"}`, sid)
	return sid
}

func callTool(t *testing.T, sid, name string, args map[string]any, id int) string {
	t.Helper()
	params := map[string]any{"name": name, "arguments": args}
	payload := map[string]any{"jsonrpc": "2.0", "id": id, "method": "tools/call", "params": params}
	data, _ := json.Marshal(payload)

	resp, err := doMCP(string(data), sid)
	if err != nil {
		t.Fatalf("tools/call %s: %v", name, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)

	// parse SSE: extract the data line
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(line, "data: ") {
			var rpc struct {
				Result struct {
					Content []struct {
						Text string `json:"text"`
					} `json:"content"`
				} `json:"result"`
				Error *struct {
					Message string `json:"message"`
				} `json:"error"`
			}
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &rpc); err == nil {
				if rpc.Error != nil {
					t.Fatalf("tools/call %s returned error: %s", name, rpc.Error.Message)
				}
				if len(rpc.Result.Content) > 0 {
					return rpc.Result.Content[0].Text
				}
			}
		}
	}
	t.Fatalf("no parseable response from tools/call %s: %s", name, raw)
	return ""
}

func doMCP(body, sessionID string) (*http.Response, error) {
	req, err := http.NewRequest("POST", addr+"/", bytes.NewReader([]byte(body)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if sessionID != "" {
		req.Header.Set("Mcp-Session-Id", sessionID)
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

func init() {
	// ensure test doesn't inherit host SCRIBE_DB
	os.Unsetenv("SCRIBE_DB")
}
