package web_test

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	parchment "github.com/dpopsuev/parchment"
	"github.com/dpopsuev/scribe/web"
)


func seedDataset(t *testing.T, s *parchment.SQLiteStore) *parchment.Protocol {
	t.Helper()
	ctx := context.Background()
	arts := []*parchment.Artifact{
		{ID: "TASK-001", Kind: "task", Scope: "alpha", Status: "active", Title: "Build export"},
		{ID: "TASK-002", Kind: "task", Scope: "alpha", Status: "draft", Title: "Draft task — excluded"},
		{ID: "ADR-001", Kind: "decision", Scope: "alpha", Status: "accepted", Title: "Use JSONL",
			Sections: []parchment.Section{
				{Name: "problem", Text: "Which format for training data?"},
				{Name: "decision", Text: "JSONL — streamable, one example per line."},
				{Name: "alternatives", Text: "CSV — not suitable for nested data."},
			},
		},
		{ID: "SPEC-001", Kind: "spec", Scope: "alpha", Status: "active", Title: "Dataset API",
			Sections: []parchment.Section{
				{Name: "problem", Text: "Need a dataset export endpoint."},
			},
		},
	}
	for _, a := range arts {
		if err := s.Put(ctx, a); err != nil {
			t.Fatal(err)
		}
	}
	_ = s.AddEdge(ctx, parchment.Edge{From: "TASK-001", To: "SPEC-001", Relation: "implements"})
	proto := parchment.New(s, nil, []string{"alpha"}, nil, parchment.ProtocolConfig{})
	return proto
}

func datasetServer(t *testing.T) *httptest.Server {
	t.Helper()
	dir := t.TempDir()
	s, err := parchment.OpenSQLite(dir + "/ds_test.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	proto := seedDataset(t, s)
	return httptest.NewServer(web.NewServer(proto, "test", ""))
}

func getJSONL(t *testing.T, srv *httptest.Server, format string) []map[string]any {
	t.Helper()
	resp, err := http.Get(srv.URL + "/api/v1/export/dataset?format=" + format)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	var rows []map[string]any
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		var row map[string]any
		if err := json.Unmarshal(sc.Bytes(), &row); err != nil {
			t.Fatalf("invalid JSON: %s — %v", sc.Text(), err)
		}
		rows = append(rows, row)
	}
	return rows
}


func TestDatasetExport_SFT_OnlyExportableArtifacts(t *testing.T) {
	srv := datasetServer(t)
	defer srv.Close()
	rows := getJSONL(t, srv, "sft")
	// draft TASK-002 must be excluded
	if len(rows) == 0 {
		t.Fatal("no rows returned")
	}
	for _, row := range rows {
		msgs, ok := row["messages"].([]any)
		if !ok || len(msgs) != 3 {
			t.Errorf("want 3 messages, got %v", row)
		}
	}
}

func TestDatasetExport_SFT_ChatTurnStructure(t *testing.T) {
	srv := datasetServer(t)
	defer srv.Close()
	rows := getJSONL(t, srv, "sft")
	for _, row := range rows {
		msgs := row["messages"].([]any)
		roles := make([]string, len(msgs))
		for i, m := range msgs {
			msg := m.(map[string]any)
			roles[i] = msg["role"].(string)
		}
		if roles[0] != "system" || roles[1] != "user" || roles[2] != "assistant" {
			t.Errorf("want [system user assistant], got %v", roles)
		}
	}
}

func TestDatasetExport_SFT_AssistantContentIsValidJSON(t *testing.T) {
	srv := datasetServer(t)
	defer srv.Close()
	rows := getJSONL(t, srv, "sft")
	for _, row := range rows {
		msgs := row["messages"].([]any)
		assistant := msgs[2].(map[string]any)
		content := assistant["content"].(string)
		var parsed map[string]any
		if err := json.Unmarshal([]byte(content), &parsed); err != nil {
			t.Errorf("assistant content is not valid JSON: %s", content)
		}
		if parsed["id"] == nil || parsed["kind"] == nil {
			t.Errorf("assistant JSON missing required fields: %v", parsed)
		}
	}
}


func TestDatasetExport_KG_ContainsNodesAndEdges(t *testing.T) {
	srv := datasetServer(t)
	defer srv.Close()
	rows := getJSONL(t, srv, "kg")

	nodeCount, edgeCount := 0, 0
	for _, row := range rows {
		if _, ok := row["head"]; ok {
			edgeCount++
		} else {
			nodeCount++
		}
	}
	if nodeCount == 0 {
		t.Error("no nodes in KG export")
	}
	if edgeCount == 0 {
		t.Error("no edges in KG export")
	}
}

func TestDatasetExport_KG_NodeHasRequiredFields(t *testing.T) {
	srv := datasetServer(t)
	defer srv.Close()
	rows := getJSONL(t, srv, "kg")
	for _, row := range rows {
		if _, ok := row["head"]; ok {
			continue // skip edges
		}
		for _, field := range []string{"id", "kind", "status", "title", "scope"} {
			if row[field] == nil {
				t.Errorf("node missing field %q: %v", field, row)
			}
		}
	}
}

func TestDatasetExport_KG_EdgeHasRequiredFields(t *testing.T) {
	srv := datasetServer(t)
	defer srv.Close()
	rows := getJSONL(t, srv, "kg")
	for _, row := range rows {
		if _, ok := row["head"]; !ok {
			continue // skip nodes
		}
		for _, field := range []string{"head", "relation", "tail"} {
			if row[field] == nil {
				t.Errorf("edge missing field %q: %v", field, row)
			}
		}
	}
}


func TestDatasetExport_DPO_OnlyDecisionArtifacts(t *testing.T) {
	srv := datasetServer(t)
	defer srv.Close()
	rows := getJSONL(t, srv, "dpo")
	if len(rows) == 0 {
		t.Fatal("no DPO examples — expected ADR-001")
	}
	for _, row := range rows {
		if row["prompt"] == nil || row["chosen"] == nil {
			t.Errorf("DPO row missing prompt or chosen: %v", row)
		}
	}
}

func TestDatasetExport_DPO_RejectedfromAlternatives(t *testing.T) {
	srv := datasetServer(t)
	defer srv.Close()
	rows := getJSONL(t, srv, "dpo")
	for _, row := range rows {
		rejected, _ := row["rejected"].(string)
		if strings.Contains(row["prompt"].(string), "Which format") && rejected == "" {
			t.Error("ADR-001 has alternatives section but rejected is empty")
		}
	}
}


func TestDatasetExport_Card_ValidMetadata(t *testing.T) {
	srv := datasetServer(t)
	defer srv.Close()
	rows := getJSONL(t, srv, "card")
	if len(rows) != 1 {
		t.Fatalf("want 1 card row, got %d", len(rows))
	}
	card := rows[0]
	for _, field := range []string{"name", "description", "license", "total_rows"} {
		if card[field] == nil {
			t.Errorf("card missing %q", field)
		}
	}
	if card["total_rows"].(float64) == 0 {
		t.Error("total_rows is 0 — quality filter may be too strict")
	}
}


func TestDatasetExport_UnknownFormat_Returns400(t *testing.T) {
	srv := datasetServer(t)
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/api/v1/export/dataset?format=xml")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("want 400, got %d", resp.StatusCode)
	}
}


func TestDatasetExport_Headers(t *testing.T) {
	srv := datasetServer(t)
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/api/v1/export/dataset?format=sft")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	ct := resp.Header.Get("Content-Type")
	if ct != "application/x-ndjson" {
		t.Errorf("Content-Type: want application/x-ndjson, got %q", ct)
	}
	if resp.Header.Get("Content-Disposition") == "" {
		t.Error("missing Content-Disposition header")
	}
}


func TestDatasetExport_QualityFilter_ExcludesDraft(t *testing.T) {
	srv := datasetServer(t)
	defer srv.Close()
	rows := getJSONL(t, srv, "sft")
	for _, row := range rows {
		msgs := row["messages"].([]any)
		assistant := msgs[2].(map[string]any)
		var artifact map[string]any
		json.Unmarshal([]byte(assistant["content"].(string)), &artifact) //nolint:errcheck // test helper — error propagates as nil map
		if artifact["id"] == "TASK-002" {
			t.Error("draft TASK-002 must not appear in SFT export")
		}
	}
}

func TestDatasetExport_QualityFilter_ExcludesViolation(t *testing.T) {
	dir := t.TempDir()
	s, _ := parchment.OpenSQLite(dir + "/viol.db")
	t.Cleanup(func() { _ = s.Close() })
	ctx := context.Background()
	// Artifact with compliance violation label
	_ = s.Put(ctx, &parchment.Artifact{
		ID: "BAD-001", Kind: "task", Scope: "x", Status: "active", Title: "Bad",
		Labels: []string{"compliance:violation"},
	})
	_ = s.Put(ctx, &parchment.Artifact{
		ID: "GOOD-001", Kind: "task", Scope: "x", Status: "active", Title: "Good",
	})
	proto := parchment.New(s, nil, []string{"x"}, nil, parchment.ProtocolConfig{})
	srv := httptest.NewServer(web.NewServer(proto, "test", ""))
	defer srv.Close()

	rows := getJSONL(t, srv, "sft")
	for _, row := range rows {
		msgs := row["messages"].([]any)
		assistant := msgs[2].(map[string]any)
		var artifact map[string]any
		json.Unmarshal([]byte(assistant["content"].(string)), &artifact) //nolint:errcheck // test helper — error propagates as nil map
		if artifact["id"] == "BAD-001" {
			t.Error("artifact with compliance:violation must be excluded")
		}
	}
}
