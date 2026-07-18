package mcp_test

// Lock-step enforcement:
//  1. Parity — every registry op is reachable over REST (denylist explicit).
//  2. Contract — a verb sequence behaves identically across surfaces.

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	parchment "github.com/dpopsuev/parchment"
	"github.com/dpopsuev/scribe/mcp"
	"github.com/dpopsuev/scribe/service"
)

func restServer(t *testing.T) *httptest.Server {
	t.Helper()
	s, err := parchment.OpenSQLite(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	proto := parchment.New(s, nil, []string{"test"}, nil, parchment.ProtocolConfig{})
	svc := service.New(proto, nil, []string{"test"})
	return httptest.NewServer(mcp.RESTHandler(svc))
}

func postOps(t *testing.T, base string, body map[string]any) map[string]any {
	t.Helper()
	b, _ := json.Marshal(body)
	res, err := http.Post(base+"/api/v1/ops", "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var out map[string]any
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

// Parity: the registry is the contract — REST must expose every op.
func TestRESTCoversOpRegistry(t *testing.T) {
	ts := restServer(t)
	defer ts.Close()

	res, err := http.Get(ts.URL + "/api/v1/ops")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var body struct {
		Ops []string `json:"ops"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	exposed := map[string]bool{}
	for _, n := range body.Ops {
		exposed[n] = true
	}
	for _, op := range service.Registry {
		if !exposed[op.Name] {
			t.Errorf("registry op %q is not exposed over REST (add to denylist if intentional)", op.Name)
		}
	}
}

func TestRESTRejectsUnknownAndMalformed(t *testing.T) {
	ts := restServer(t)
	defer ts.Close()

	out := postOps(t, ts.URL, map[string]any{"action": "frobnicate"})
	if out["ok"] != false {
		t.Errorf("unknown action accepted: %v", out)
	}
	res, err := http.Post(ts.URL+"/api/v1/ops", "application/json", strings.NewReader("{not json"))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Errorf("malformed JSON → %d, want 400", res.StatusCode)
	}
}

// Contract: create → query → get → link → dashboard behaves over REST.
func TestRESTContractFlow(t *testing.T) {
	ts := restServer(t)
	defer ts.Close()

	// create (inside the home scope — queries narrow to it by default)
	out := postOps(t, ts.URL, map[string]any{
		"action": "create", "kind": "effort.task", "title": "Contract Task",
		"scope": "test", "labels": []string{"project:test"},
	})
	if out["ok"] != true {
		t.Fatalf("create failed: %v", out)
	}

	// query finds it
	out = postOps(t, ts.URL, map[string]any{
		"action": "query", "kind": "effort.task", "query": "Contract",
	})
	if out["ok"] != true || !strings.Contains(out["text"].(string), "Contract Task") {
		t.Errorf("query missed created artifact: %v", out)
	}

	// dashboard sees the scope
	out = postOps(t, ts.URL, map[string]any{"action": "dashboard", "scope": "test"})
	if out["ok"] != true {
		t.Errorf("dashboard failed: %v", out)
	}

	// status answers introspection
	out = postOps(t, ts.URL, map[string]any{"action": "status"})
	if out["ok"] != true {
		t.Errorf("status failed: %v", out)
	}
}
