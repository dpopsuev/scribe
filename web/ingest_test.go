package web_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	parchment "github.com/dpopsuev/parchment"

	"github.com/dpopsuev/scribe/internal/ingest"
	"github.com/dpopsuev/scribe/web"
)

func newTestServer(t *testing.T) (*httptest.Server, parchment.Store) {
	t.Helper()
	db, err := parchment.OpenSQLite(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	proto := parchment.New(db, nil, nil, nil, parchment.ProtocolConfig{})
	return httptest.NewServer(web.NewServer(proto, "test", "")), db
}

func countByPrefix(t *testing.T, db parchment.Store, prefix string) int {
	t.Helper()
	arts, err := db.List(context.Background(), parchment.Filter{IDPrefix: prefix})
	if err != nil {
		t.Fatalf("list %q: %v", prefix, err)
	}
	return len(arts)
}

// parseGenerated reads NDJSON emitted by Generator.WriteTo into typed slices.
func parseGenerated(t *testing.T, gen *ingest.Generator) ([]ingest.NodeRecord, []ingest.EdgeRecord) {
	t.Helper()
	var buf bytes.Buffer
	if _, _, err := gen.Generate(&buf); err != nil {
		t.Fatalf("generate: %v", err)
	}
	var nodes []ingest.NodeRecord
	var edges []ingest.EdgeRecord
	sc := bufio.NewScanner(&buf)
	for sc.Scan() {
		var envelope struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(sc.Bytes(), &envelope); err != nil {
			continue
		}
		switch envelope.Type {
		case "node":
			var n ingest.NodeRecord
			if err := json.Unmarshal(sc.Bytes(), &n); err == nil {
				nodes = append(nodes, n)
			}
		case "edge":
			var e ingest.EdgeRecord
			if err := json.Unmarshal(sc.Bytes(), &e); err == nil {
				edges = append(edges, e)
			}
		}
	}
	return nodes, edges
}

func TestIngest_Scale(t *testing.T) {
	srv, db := newTestServer(t)
	defer srv.Close()

	gen := &ingest.Generator{Source: "locus", NodeCount: 200, EdgesPerNode: 5, Shape: ingest.LocusComponentShape}
	nodes, edges := parseGenerated(t, gen)

	start := time.Now()
	result, err := (&ingest.Client{BaseURL: srv.URL, Source: "locus"}).Stream(context.Background(), nodes, edges)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	if len(result.Errors) > 0 {
		t.Errorf("errors: %v", result.Errors)
	}
	if result.Inserted != gen.NodeCount {
		t.Errorf("inserted=%d want %d", result.Inserted, gen.NodeCount)
	}
	if elapsed > 3*time.Second {
		t.Errorf("took %v, want < 3s", elapsed)
	}
	if got := countByPrefix(t, db, "locus:"); got != gen.NodeCount {
		t.Errorf("parchment has %d nodes, want %d", got, gen.NodeCount)
	}
}

func TestIngest_Idempotency(t *testing.T) {
	srv, db := newTestServer(t)
	defer srv.Close()

	gen := &ingest.Generator{Source: "locus", NodeCount: 50, ScanSHA: "fixed-sha", Shape: ingest.LocusComponentShape}
	nodes, edges := parseGenerated(t, gen)
	client := &ingest.Client{BaseURL: srv.URL, Source: "locus"}

	for i, label := range []string{"first", "second"} {
		r, err := client.Stream(context.Background(), nodes, edges)
		if err != nil || len(r.Errors) > 0 {
			t.Fatalf("run %d (%s): err=%v errors=%v", i+1, label, err, r.Errors)
		}
	}
	if got := countByPrefix(t, db, "locus:"); got != gen.NodeCount {
		t.Errorf("after two runs: %d nodes, want %d", got, gen.NodeCount)
	}
}

func TestIngest_MultiSource(t *testing.T) {
	srv, db := newTestServer(t)
	defer srv.Close()

	cases := []struct {
		source string
		shape  ingest.ShapeFunc
		count  int
	}{
		{"locus", ingest.LocusComponentShape, 30},
		{"jira", ingest.JiraIssueShape, 20},
		{"github", ingest.GitHubPRShape, 10},
	}
	for _, c := range cases {
		gen := &ingest.Generator{Source: c.source, NodeCount: c.count, Shape: c.shape}
		nodes, edges := parseGenerated(t, gen)
		r, err := (&ingest.Client{BaseURL: srv.URL, Source: c.source}).Stream(context.Background(), nodes, edges)
		if err != nil || len(r.Errors) > 0 {
			t.Errorf("source=%s: err=%v errors=%v", c.source, err, r.Errors)
		}
		if got := countByPrefix(t, db, c.source+":"); got != c.count {
			t.Errorf("source=%s: %d nodes, want %d", c.source, got, c.count)
		}
	}
}
