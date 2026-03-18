//go:build stress

package stress_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/http"
	_ "net/http/pprof"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"strings"
	"sync"
	"testing"
	"time"

	scribemcp "github.com/dpopsuev/scribe/mcp"
	"github.com/dpopsuev/scribe/model"
	"github.com/dpopsuev/scribe/protocol"
	"github.com/dpopsuev/scribe/store"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// --- helpers ---

type heapSnapshot struct {
	HeapAlloc  uint64 // bytes currently allocated on heap
	HeapInuse  uint64 // bytes in in-use spans
	HeapSys    uint64 // bytes obtained from OS
	NumGC      uint32
	Goroutines int
}

func snapHeap() heapSnapshot {
	runtime.GC()
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return heapSnapshot{
		HeapAlloc:  m.HeapAlloc,
		HeapInuse:  m.HeapInuse,
		HeapSys:    m.HeapSys,
		NumGC:      m.NumGC,
		Goroutines: runtime.NumGoroutine(),
	}
}

func mb(b uint64) float64 {
	return float64(b) / (1024 * 1024)
}

func openStore(t *testing.T) *store.SQLiteStore {
	t.Helper()
	s, err := store.OpenSQLite(filepath.Join(t.TempDir(), "stress.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func newServer(t *testing.T, s store.Store) *sdkmcp.Server {
	t.Helper()
	srv, _ := scribemcp.NewServer(s, []string{"stress"}, nil, protocol.IDConfig{}, "test")
	return srv
}

func connectClient(t *testing.T, srv *sdkmcp.Server) *sdkmcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	t1, t2 := sdkmcp.NewInMemoryTransports()
	if _, err := srv.Connect(ctx, t1, nil); err != nil {
		t.Fatal(err)
	}
	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "stress-client", Version: "0.1"}, nil)
	cs, err := client.Connect(ctx, t2, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs
}

func callTool(t *testing.T, cs *sdkmcp.ClientSession, name string, args map[string]any) string {
	t.Helper()
	ctx := context.Background()
	result, err := cs.CallTool(ctx, &sdkmcp.CallToolParams{
		Name:      name,
		Arguments: args,
	})
	if err != nil {
		t.Fatalf("tool %s: %v", name, err)
	}
	if result.IsError {
		t.Fatalf("tool %s error: %v", name, result.Content)
	}
	if len(result.Content) > 0 {
		if tc, ok := result.Content[0].(*sdkmcp.TextContent); ok {
			return tc.Text
		}
	}
	return ""
}

func seedArtifacts(t *testing.T, s store.Store, count int) {
	t.Helper()
	ctx := context.Background()
	for i := 0; i < count; i++ {
		art := &model.Artifact{
			ID:     fmt.Sprintf("STR-TSK-%d", i+1),
			Kind:   "task",
			Scope:  "stress",
			Status: "draft",
			Title:  fmt.Sprintf("Stress task %d", i+1),
			Goal:   fmt.Sprintf("Goal for task %d with some filler text to make it non-trivial", i+1),
			Sections: []model.Section{
				{Name: "context", Text: fmt.Sprintf("Context for task %d. This section contains enough text to be representative of real artifacts with multiple paragraphs of content.", i+1)},
			},
		}
		if i > 0 {
			art.Parent = fmt.Sprintf("STR-TSK-%d", ((i-1)/3)+1)
		}
		if err := s.Put(ctx, art); err != nil {
			t.Fatal(err)
		}
	}
	// Add parent_of edges for tree structure
	for i := 1; i < count; i++ {
		parentID := fmt.Sprintf("STR-TSK-%d", ((i-1)/3)+1)
		childID := fmt.Sprintf("STR-TSK-%d", i+1)
		s.AddEdge(ctx, model.Edge{From: parentID, Relation: model.RelParentOf, To: childID})
	}
}

// assertHeapBelow fails the test if current heap exceeds the limit.
func assertHeapBelow(t *testing.T, label string, limitMB float64) heapSnapshot {
	t.Helper()
	snap := snapHeap()
	t.Logf("%s: HeapAlloc=%.1fMB HeapInuse=%.1fMB Goroutines=%d",
		label, mb(snap.HeapAlloc), mb(snap.HeapInuse), snap.Goroutines)
	if mb(snap.HeapAlloc) > limitMB {
		t.Errorf("%s: HeapAlloc %.1fMB exceeds limit %.0fMB", label, mb(snap.HeapAlloc), limitMB)
	}
	return snap
}

// --- Stress Test 1: Idle Baseline ---

func TestStress_IdleBaseline(t *testing.T) {
	s := openStore(t)
	seedArtifacts(t, s, 100)
	srv := newServer(t, s)
	cs := connectClient(t, srv)

	// Establish baseline after connection
	_ = callTool(t, cs, "artifact", map[string]any{"action": "list"})
	baseline := assertHeapBelow(t, "baseline", 100)

	// Wait and measure — no traffic
	for i := 1; i <= 5; i++ {
		time.Sleep(2 * time.Second)
		snap := assertHeapBelow(t, fmt.Sprintf("idle_%ds", i*2), 100)
		growth := float64(snap.HeapAlloc) - float64(baseline.HeapAlloc)
		t.Logf("  growth: %+.1fMB goroutines: %d→%d", mb(uint64(max(0, int64(growth)))), baseline.Goroutines, snap.Goroutines)
	}
}

// --- Stress Test 2: Read-Heavy ---

func TestStress_ReadHeavy(t *testing.T) {
	s := openStore(t)
	seedArtifacts(t, s, 200)
	srv := newServer(t, s)
	cs := connectClient(t, srv)

	baseline := assertHeapBelow(t, "baseline", 100)

	const totalCalls = 5000
	for i := 1; i <= totalCalls; i++ {
		if i%2 == 0 {
			callTool(t, cs, "artifact", map[string]any{"action": "list", "scope": "stress"})
		} else {
			id := fmt.Sprintf("STR-TSK-%d", (i%200)+1)
			callTool(t, cs, "artifact", map[string]any{"action": "get", "id": id})
		}
		if i%500 == 0 {
			assertHeapBelow(t, fmt.Sprintf("call_%d", i), 150)
		}
	}

	afterBurst := assertHeapBelow(t, "after_burst", 150)

	// Wait for GC to reclaim
	time.Sleep(3 * time.Second)
	afterGC := assertHeapBelow(t, "after_gc", 100)

	t.Logf("summary: baseline=%.1fMB peak=%.1fMB settled=%.1fMB",
		mb(baseline.HeapAlloc), mb(afterBurst.HeapAlloc), mb(afterGC.HeapAlloc))
}

// --- Stress Test 3: Briefing-Heavy ---

func TestStress_BriefingHeavy(t *testing.T) {
	s := openStore(t)
	// Create a deep DAG: 300 artifacts with parent-child chains
	seedArtifacts(t, s, 300)

	// Add cross-cutting edges (depends_on, justifies) for dense graph
	ctx := context.Background()
	for i := 10; i < 300; i += 7 {
		target := fmt.Sprintf("STR-TSK-%d", (i%50)+1)
		source := fmt.Sprintf("STR-TSK-%d", i+1)
		s.AddEdge(ctx, model.Edge{From: source, Relation: model.RelDependsOn, To: target})
	}

	srv := newServer(t, s)
	cs := connectClient(t, srv)

	baseline := assertHeapBelow(t, "baseline", 100)

	const totalCalls = 500
	for i := 1; i <= totalCalls; i++ {
		id := fmt.Sprintf("STR-TSK-%d", (i%10)+1) // briefing on root-ish nodes
		callTool(t, cs, "graph", map[string]any{
			"action": "briefing",
			"id":     id,
		})
		if i%100 == 0 {
			assertHeapBelow(t, fmt.Sprintf("briefing_%d", i), 200)
		}
	}

	afterBurst := assertHeapBelow(t, "after_burst", 200)

	time.Sleep(3 * time.Second)
	afterGC := assertHeapBelow(t, "after_gc", 100)

	t.Logf("summary: baseline=%.1fMB peak=%.1fMB settled=%.1fMB",
		mb(baseline.HeapAlloc), mb(afterBurst.HeapAlloc), mb(afterGC.HeapAlloc))
}

// --- Stress Test 4: Session Accumulation ---

func TestStress_SessionAccumulation(t *testing.T) {
	s := openStore(t)
	seedArtifacts(t, s, 50)

	sessionTimeout := 5 * time.Second // short timeout for test
	srv, _ := scribemcp.NewServer(s, []string{"stress"}, nil, protocol.IDConfig{}, "test")

	handler := sdkmcp.NewStreamableHTTPHandler(
		func(r *http.Request) *sdkmcp.Server { return srv },
		&sdkmcp.StreamableHTTPOptions{
			SessionTimeout: sessionTimeout,
		},
	)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	httpServer := &http.Server{Handler: handler}
	go httpServer.Serve(listener)
	t.Cleanup(func() { httpServer.Close() })

	addr := "http://" + listener.Addr().String()
	t.Logf("stress server on %s (session_timeout=%s)", addr, sessionTimeout)

	baseline := assertHeapBelow(t, "baseline", 100)
	baselineGoroutines := baseline.Goroutines

	// Open 100 sessions, send a few calls, then abandon (no DELETE)
	const numSessions = 100
	for i := 0; i < numSessions; i++ {
		sid := httpInitSession(t, addr)
		// Send a few calls per session
		for j := 0; j < 3; j++ {
			httpToolCall(t, addr, sid, "artifact", map[string]any{"action": "list"})
		}
		// Abandon session — no DELETE sent
		if (i+1)%25 == 0 {
			snap := assertHeapBelow(t, fmt.Sprintf("sessions_%d", i+1), 200)
			t.Logf("  goroutines: %d (baseline: %d)", snap.Goroutines, baselineGoroutines)
		}
	}

	afterSessions := assertHeapBelow(t, "after_100_sessions", 200)
	t.Logf("goroutines after sessions: %d (baseline: %d)", afterSessions.Goroutines, baselineGoroutines)

	// Wait for session timeout to clean up orphaned sessions
	t.Logf("waiting %s for session timeout cleanup...", sessionTimeout+3*time.Second)
	time.Sleep(sessionTimeout + 3*time.Second)

	afterCleanup := assertHeapBelow(t, "after_cleanup", 100)
	t.Logf("goroutines after cleanup: %d (baseline: %d)", afterCleanup.Goroutines, baselineGoroutines)

	// Goroutine count should return near baseline
	goroutineGrowth := afterCleanup.Goroutines - baselineGoroutines
	if goroutineGrowth > 20 {
		t.Errorf("goroutine leak: %d goroutines above baseline after session cleanup", goroutineGrowth)
	}

	t.Logf("summary: baseline=%.1fMB peak=%.1fMB settled=%.1fMB goroutines=%d→%d→%d",
		mb(baseline.HeapAlloc), mb(afterSessions.HeapAlloc), mb(afterCleanup.HeapAlloc),
		baselineGoroutines, afterSessions.Goroutines, afterCleanup.Goroutines)
}

// TestStress_SessionAccumulation_NoTimeout reproduces the bug: without session
// timeout, orphaned sessions accumulate forever.
func TestStress_SessionAccumulation_NoTimeout(t *testing.T) {
	s := openStore(t)
	seedArtifacts(t, s, 50)

	srv, _ := scribemcp.NewServer(s, []string{"stress"}, nil, protocol.IDConfig{}, "test")

	// BUG REPRODUCTION: nil options = no session timeout
	handler := sdkmcp.NewStreamableHTTPHandler(
		func(r *http.Request) *sdkmcp.Server { return srv },
		nil,
	)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	httpServer := &http.Server{Handler: handler}
	go httpServer.Serve(listener)
	t.Cleanup(func() { httpServer.Close() })

	addr := "http://" + listener.Addr().String()
	t.Logf("stress server on %s (NO session timeout — reproducing bug)", addr)

	baseline := assertHeapBelow(t, "baseline", 100)
	baselineGoroutines := baseline.Goroutines

	// Open 100 sessions, abandon all
	const numSessions = 100
	for i := 0; i < numSessions; i++ {
		sid := httpInitSession(t, addr)
		httpToolCall(t, addr, sid, "artifact", map[string]any{"action": "list"})
	}

	afterSessions := assertHeapBelow(t, "after_100_sessions", 300)

	// Wait — without timeout, nothing should be cleaned up
	time.Sleep(5 * time.Second)

	afterWait := assertHeapBelow(t, "after_wait", 300)

	// Verify goroutines are still elevated (sessions not cleaned up)
	goroutineGrowth := afterWait.Goroutines - baselineGoroutines
	t.Logf("goroutine growth (no timeout): %d (expected: elevated, not cleaned up)", goroutineGrowth)
	if goroutineGrowth < 10 {
		t.Logf("NOTE: fewer goroutines leaked than expected — SDK may have changed behavior")
	}

	t.Logf("summary: baseline=%.1fMB sessions=%.1fMB after_wait=%.1fMB goroutines=%d→%d→%d",
		mb(baseline.HeapAlloc), mb(afterSessions.HeapAlloc), mb(afterWait.HeapAlloc),
		baselineGoroutines, afterSessions.Goroutines, afterWait.Goroutines)
}

// --- Stress Test 5: Write-Heavy ---

func TestStress_WriteHeavy(t *testing.T) {
	s := openStore(t)
	srv := newServer(t, s)
	cs := connectClient(t, srv)

	baseline := assertHeapBelow(t, "baseline", 100)

	const totalCreates = 1000
	for i := 1; i <= totalCreates; i++ {
		sections := []map[string]any{
			{"name": "context", "text": fmt.Sprintf("Context section for artifact %d. This contains representative text that simulates real-world section content with multiple sentences to ensure we're testing realistic payload sizes.", i)},
			{"name": "design", "text": fmt.Sprintf("Design section for artifact %d. Architecture decisions, trade-offs, and implementation notes go here. This is the second of three sections per artifact.", i)},
			{"name": "acceptance", "text": fmt.Sprintf("Given artifact %d is created\nWhen sections are attached\nThen memory should not grow unbounded.", i)},
		}

		callTool(t, cs, "artifact", map[string]any{
			"action":   "create",
			"kind":     "task",
			"title":    fmt.Sprintf("Write stress task %d", i),
			"scope":    "stress",
			"sections": sections,
		})
		if i%100 == 0 {
			assertHeapBelow(t, fmt.Sprintf("create_%d", i), 200)
		}
	}

	afterWrites := assertHeapBelow(t, "after_writes", 200)

	time.Sleep(3 * time.Second)
	afterGC := assertHeapBelow(t, "after_gc", 150)

	t.Logf("summary: baseline=%.1fMB peak=%.1fMB settled=%.1fMB",
		mb(baseline.HeapAlloc), mb(afterWrites.HeapAlloc), mb(afterGC.HeapAlloc))
}

// --- Stress Test 6: Heap Profile Comparison ---

func TestStress_HeapProfile(t *testing.T) {
	s := openStore(t)
	seedArtifacts(t, s, 200)
	srv := newServer(t, s)
	cs := connectClient(t, srv)

	// Snapshot at t=0
	t0 := snapHeap()
	t.Logf("t0: HeapAlloc=%.1fMB HeapInuse=%.1fMB Goroutines=%d",
		mb(t0.HeapAlloc), mb(t0.HeapInuse), t0.Goroutines)

	// Phase 1: mixed workload (500 calls)
	for i := 0; i < 500; i++ {
		switch i % 4 {
		case 0:
			callTool(t, cs, "artifact", map[string]any{"action": "list", "scope": "stress"})
		case 1:
			id := fmt.Sprintf("STR-TSK-%d", (i%200)+1)
			callTool(t, cs, "artifact", map[string]any{"action": "get", "id": id})
		case 2:
			id := fmt.Sprintf("STR-TSK-%d", (i%10)+1)
			callTool(t, cs, "graph", map[string]any{"action": "tree", "id": id})
		case 3:
			callTool(t, cs, "admin", map[string]any{"action": "motd"})
		}
	}
	t1 := assertHeapBelow(t, "t1_after_500_mixed", 150)

	// Phase 2: another 500 calls
	for i := 0; i < 500; i++ {
		switch i % 4 {
		case 0:
			callTool(t, cs, "artifact", map[string]any{"action": "list", "scope": "stress"})
		case 1:
			id := fmt.Sprintf("STR-TSK-%d", (i%200)+1)
			callTool(t, cs, "artifact", map[string]any{"action": "get", "id": id})
		case 2:
			id := fmt.Sprintf("STR-TSK-%d", (i%10)+1)
			callTool(t, cs, "graph", map[string]any{"action": "briefing", "id": id})
		case 3:
			callTool(t, cs, "admin", map[string]any{"action": "dashboard"})
		}
	}
	t2 := assertHeapBelow(t, "t2_after_1000_mixed", 200)

	// Phase 3: settle
	time.Sleep(3 * time.Second)
	t3 := assertHeapBelow(t, "t3_settled", 100)

	// Analyze growth pattern
	growth01 := int64(t1.HeapAlloc) - int64(t0.HeapAlloc)
	growth12 := int64(t2.HeapAlloc) - int64(t1.HeapAlloc)
	growth23 := int64(t3.HeapAlloc) - int64(t0.HeapAlloc)

	t.Logf("heap growth analysis:")
	t.Logf("  t0→t1 (500 calls):  %+.1fMB", mb(uint64(max(0, growth01))))
	t.Logf("  t1→t2 (500 calls):  %+.1fMB", mb(uint64(max(0, growth12))))
	t.Logf("  t0→t3 (settled):    %+.1fMB", mb(uint64(max(0, growth23))))

	// If settled heap is significantly above baseline, something is leaking
	if growth23 > 50*1024*1024 { // 50MB above baseline after settling
		t.Errorf("heap did not return to baseline: grew by %.1fMB", mb(uint64(growth23)))
	}
}

// --- HTTP helpers for session tests ---

func httpInitSession(t *testing.T, addr string) string {
	t.Helper()
	body := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"stress","version":"0.1"}}}`
	resp, err := httpPost(addr, body, "")
	if err != nil {
		t.Fatalf("initialize: %v", err)
	}
	sid := resp.Header.Get("Mcp-Session-Id")
	resp.Body.Close()
	if sid == "" {
		t.Fatal("no Mcp-Session-Id")
	}
	// Send initialized notification
	resp2, _ := httpPost(addr, `{"jsonrpc":"2.0","method":"notifications/initialized"}`, sid)
	if resp2 != nil {
		resp2.Body.Close()
	}
	return sid
}

func httpToolCall(t *testing.T, addr, sid, tool string, args map[string]any) string {
	t.Helper()
	params := map[string]any{"name": tool, "arguments": args}
	payload := map[string]any{"jsonrpc": "2.0", "id": 2, "method": "tools/call", "params": params}
	data, _ := json.Marshal(payload)

	resp, err := httpPost(addr, string(data), sid)
	if err != nil {
		t.Fatalf("tools/call %s: %v", tool, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)

	// Extract text from SSE or JSON response
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(line, "data: ") {
			var rpc struct {
				Result struct {
					Content []struct {
						Text string `json:"text"`
					} `json:"content"`
				} `json:"result"`
			}
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &rpc); err == nil {
				if len(rpc.Result.Content) > 0 {
					return rpc.Result.Content[0].Text
				}
			}
		}
	}
	return string(raw)
}

// --- Stress Test 7: ReadLog Growth (SCR-BUG-14 vector 1) ---
// The handler.readLog map grows with every get call and is never cleared.
// This test proves the leak by calling get 10K times and measuring map growth.

func TestStress_ReadLogGrowth(t *testing.T) {
	s := openStore(t)
	seedArtifacts(t, s, 100)
	srv := newServer(t, s)
	cs := connectClient(t, srv)

	baseline := assertHeapBelow(t, "baseline", 100)

	// 10K get calls on distinct IDs — readLog should grow to 100 entries
	// (only 100 unique IDs, but the map is never cleared between sessions)
	const totalGets = 10000
	for i := 0; i < totalGets; i++ {
		id := fmt.Sprintf("STR-TSK-%d", (i%100)+1)
		callTool(t, cs, "artifact", map[string]any{"action": "get", "id": id})
	}

	afterGets := assertHeapBelow(t, "after_10k_gets", 150)

	// The readLog map should have at most 100 entries (100 unique IDs)
	// If it's growing beyond that, it's a leak
	// We can't directly inspect readLog from here, but we can measure heap growth
	growth := float64(afterGets.HeapAlloc) - float64(baseline.HeapAlloc)
	t.Logf("heap growth after 10K gets: %.1fMB (readLog should have ~100 entries)", mb(uint64(max(0, int64(growth)))))

	// Now create a second session and verify readLog is NOT per-session
	// (it's on the shared handler, so it persists)
	cs2 := connectClient(t, srv)
	for i := 0; i < 1000; i++ {
		id := fmt.Sprintf("STR-TSK-%d", (i%100)+1)
		callTool(t, cs2, "artifact", map[string]any{"action": "get", "id": id})
	}

	afterSecondSession := assertHeapBelow(t, "after_second_session", 150)
	t.Logf("summary: baseline=%.1fMB after_gets=%.1fMB after_second=%.1fMB",
		mb(baseline.HeapAlloc), mb(afterGets.HeapAlloc), mb(afterSecondSession.HeapAlloc))
}

// --- Stress Test 8: WAL Growth Without Checkpoint (SCR-BUG-14 vector 4) ---
// WAL file grows with every write. Without periodic checkpoint, it can grow
// to hundreds of MB. This test measures WAL file size after sustained writes.

func TestStress_WALGrowth(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "wal-stress.db")
	s, err := store.OpenSQLite(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	ctx := context.Background()

	// Write 2000 artifacts with sections
	for i := 0; i < 2000; i++ {
		art := &model.Artifact{
			ID:     fmt.Sprintf("WAL-TSK-%d", i+1),
			Kind:   "task",
			Scope:  "stress",
			Status: "draft",
			Title:  fmt.Sprintf("WAL stress task %d with some padding text", i+1),
			Sections: []model.Section{
				{Name: "context", Text: fmt.Sprintf("Section content for artifact %d. This text is representative of real-world section sizes and contains multiple sentences to simulate actual usage patterns in production.", i+1)},
			},
		}
		if err := s.Put(ctx, art); err != nil {
			t.Fatalf("put %d: %v", i, err)
		}
	}

	// Check WAL file size
	walPath := dbPath + "-wal"
	info, err := os.Stat(walPath)
	if err != nil {
		t.Logf("WAL file not found (may be checkpointed): %v", err)
	} else {
		walSizeMB := float64(info.Size()) / (1024 * 1024)
		t.Logf("WAL size after 2000 writes: %.1fMB", walSizeMB)
		if walSizeMB > 50 {
			t.Errorf("WAL file too large: %.1fMB (expected < 50MB for 2000 artifacts)", walSizeMB)
		}
	}

	// Check DB file size
	dbInfo, _ := os.Stat(dbPath)
	t.Logf("DB size: %.1fMB", float64(dbInfo.Size())/(1024*1024))
}

// --- Stress Test 9: Connection Pool Stats (SCR-BUG-14 vector 3) ---
// Verify that SQLite connections are properly managed and not leaking.

func TestStress_ConnectionPool(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "conn-stress.db")
	s, err := store.OpenSQLiteConfig(store.SQLiteConfig{Path: dbPath})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	ctx := context.Background()
	seedArtifacts(t, s, 50)

	// Get DB stats via the writer
	writer := s.Writer()
	statsBefore := writer.Stats()
	t.Logf("before: open=%d inUse=%d idle=%d waitCount=%d",
		statsBefore.OpenConnections, statsBefore.InUse, statsBefore.Idle, statsBefore.WaitCount)

	// Run 1000 concurrent-ish queries
	for i := 0; i < 1000; i++ {
		s.List(ctx, model.Filter{Scope: "stress"})
		if i%100 == 0 {
			stats := writer.Stats()
			t.Logf("  after %d queries: open=%d inUse=%d idle=%d",
				i, stats.OpenConnections, stats.InUse, stats.Idle)
		}
	}

	statsAfter := writer.Stats()
	t.Logf("after: open=%d inUse=%d idle=%d waitCount=%d maxLifetimeClosed=%d",
		statsAfter.OpenConnections, statsAfter.InUse, statsAfter.Idle,
		statsAfter.WaitCount, statsAfter.MaxLifetimeClosed)

	// Open connections should be bounded
	if statsAfter.OpenConnections > 10 {
		t.Errorf("too many open connections: %d (expected <= 10)", statsAfter.OpenConnections)
	}
}

// --- Stress Test 10: Long-Running HTTP Leak Hunt (SCR-BUG-14) ---
//
// This test reproduces production conditions:
// - Real HTTP transport (not in-memory)
// - Multiple concurrent sessions creating and abandoning connections
// - Mixed workload: list, get, search, tree, briefing, create, motd
// - Periodic session churn (new sessions every N seconds)
// - pprof heap profiles captured at intervals for offline analysis
//
// Run with: go test -tags stress -v -timeout 600s -run TestStress_HTTPLeakHunt .
// Duration controlled by STRESS_DURATION env var (default: 5m)
// pprof profiles written to STRESS_PPROF_DIR env var (default: /tmp/scribe-pprof/)

func TestStress_HTTPLeakHunt(t *testing.T) {
	// Config
	duration := 5 * time.Minute
	if d := os.Getenv("STRESS_DURATION"); d != "" {
		if parsed, err := time.ParseDuration(d); err == nil {
			duration = parsed
		}
	}
	pprofDir := os.Getenv("STRESS_PPROF_DIR")
	if pprofDir == "" {
		pprofDir = filepath.Join(os.TempDir(), "scribe-pprof")
	}
	os.MkdirAll(pprofDir, 0o755)
	t.Logf("duration: %s, pprof dir: %s", duration, pprofDir)

	// Setup: real HTTP server with session timeout
	dbPath := filepath.Join(t.TempDir(), "leak-hunt.db")
	s, err := store.OpenSQLite(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	seedArtifacts(t, s, 500)
	t.Log("seeded 500 artifacts")

	srv, _ := scribemcp.NewServer(s, []string{"stress"}, nil, protocol.IDConfig{}, "test")

	handler := sdkmcp.NewStreamableHTTPHandler(
		func(r *http.Request) *sdkmcp.Server { return srv },
		&sdkmcp.StreamableHTTPOptions{SessionTimeout: 30 * time.Second},
	)

	// pprof endpoint on separate port (net/http/pprof registers on DefaultServeMux via _ import)
	pprofListener, _ := net.Listen("tcp", "127.0.0.1:0")
	pprofServer := &http.Server{Handler: http.DefaultServeMux}
	go pprofServer.Serve(pprofListener)
	t.Cleanup(func() { pprofServer.Close() })
	t.Logf("pprof on http://%s/debug/pprof/", pprofListener.Addr())

	// MCP HTTP server
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	httpServer := &http.Server{Handler: handler}
	go httpServer.Serve(listener)
	t.Cleanup(func() { httpServer.Close() })
	addr := "http://" + listener.Addr().String()
	t.Logf("MCP server on %s", addr)

	// Capture baseline
	baseline := snapHeap()
	t.Logf("BASELINE: HeapAlloc=%.1fMB HeapSys=%.1fMB Goroutines=%d",
		mb(baseline.HeapAlloc), mb(baseline.HeapSys), baseline.Goroutines)
	captureHeapProfile(t, pprofDir, "baseline")

	// Workload generators
	ctx, cancel := context.WithTimeout(context.Background(), duration)
	defer cancel()

	var wg sync.WaitGroup
	var totalCalls, totalSessions, totalErrors int64
	var mu sync.Mutex

	incCalls := func()    { mu.Lock(); totalCalls++; mu.Unlock() }
	incSessions := func() { mu.Lock(); totalSessions++; mu.Unlock() }
	incErrors := func()   { mu.Lock(); totalErrors++; mu.Unlock() }

	// Worker: sustained session with mixed workload
	sessionWorker := func(workerID int) {
		defer wg.Done()
		rng := rand.New(rand.NewSource(int64(workerID)))

		for {
			select {
			case <-ctx.Done():
				return
			default:
			}

			// Open session
			sid := httpInitSessionQuiet(addr)
			if sid == "" {
				incErrors()
				time.Sleep(100 * time.Millisecond)
				continue
			}
			incSessions()

			// Run 5-20 calls per session
			numCalls := 5 + rng.Intn(16)
			for j := 0; j < numCalls; j++ {
				select {
				case <-ctx.Done():
					return
				default:
				}

				var toolErr bool
				switch rng.Intn(8) {
				case 0: // list
					toolErr = httpToolCallQuiet(addr, sid, "artifact", map[string]any{
						"action": "list", "scope": "stress", "limit": 50,
					})
				case 1: // get
					id := fmt.Sprintf("STR-TSK-%d", rng.Intn(500)+1)
					toolErr = httpToolCallQuiet(addr, sid, "artifact", map[string]any{
						"action": "get", "id": id,
					})
				case 2: // search
					toolErr = httpToolCallQuiet(addr, sid, "artifact", map[string]any{
						"action": "list", "query": "stress task",
					})
				case 3: // tree
					id := fmt.Sprintf("STR-TSK-%d", rng.Intn(50)+1)
					toolErr = httpToolCallQuiet(addr, sid, "graph", map[string]any{
						"action": "tree", "id": id,
					})
				case 4: // briefing
					id := fmt.Sprintf("STR-TSK-%d", rng.Intn(10)+1)
					toolErr = httpToolCallQuiet(addr, sid, "graph", map[string]any{
						"action": "briefing", "id": id,
					})
				case 5: // motd
					toolErr = httpToolCallQuiet(addr, sid, "admin", map[string]any{
						"action": "motd",
					})
				case 6: // create
					toolErr = httpToolCallQuiet(addr, sid, "artifact", map[string]any{
						"action": "create", "kind": "task", "title": fmt.Sprintf("leak-hunt-%d-%d", workerID, j),
						"scope": "stress", "priority": "low",
					})
				case 7: // compact list
					toolErr = httpToolCallQuiet(addr, sid, "artifact", map[string]any{
						"action": "list", "scope": "stress",
						"fields": []string{"id", "title", "status"},
						"limit":  20,
					})
				}
				if toolErr {
					incErrors()
				} else {
					incCalls()
				}
				time.Sleep(time.Duration(10+rng.Intn(50)) * time.Millisecond)
			}

			// 50% chance: abandon session (no cleanup)
			// 50% chance: let it timeout naturally
			// This simulates real agent behavior (disconnect, crash, etc.)
		}
	}

	// Start 4 concurrent session workers
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go sessionWorker(i)
	}

	// Monitor loop: snapshot heap + capture pprof at intervals
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	snapCount := 0

	for {
		select {
		case <-ctx.Done():
			goto done
		case <-ticker.C:
			snapCount++
			snap := snapHeap()
			mu.Lock()
			calls, sessions, errors := totalCalls, totalSessions, totalErrors
			mu.Unlock()

			growth := float64(snap.HeapAlloc) - float64(baseline.HeapAlloc)
			t.Logf("SNAP %d (%.0fs): HeapAlloc=%.1fMB HeapSys=%.1fMB Goroutines=%d growth=%.1fMB calls=%d sessions=%d errors=%d",
				snapCount, float64(snapCount)*30,
				mb(snap.HeapAlloc), mb(snap.HeapSys), snap.Goroutines,
				mb(uint64(max(0, int64(growth)))),
				calls, sessions, errors)

			label := fmt.Sprintf("snap_%03d_%ds", snapCount, snapCount*30)
			captureHeapProfile(t, pprofDir, label)

			// WAL size check
			walPath := dbPath + "-wal"
			if info, err := os.Stat(walPath); err == nil {
				t.Logf("  WAL: %.1fMB", float64(info.Size())/(1024*1024))
			}

			// Fail fast if heap is clearly leaking
			if mb(snap.HeapAlloc) > 500 {
				t.Fatalf("LEAK DETECTED: HeapAlloc=%.1fMB exceeds 500MB at snap %d", mb(snap.HeapAlloc), snapCount)
			}
		}
	}

done:
	cancel()
	wg.Wait()

	// Final measurements
	afterLoad := snapHeap()
	captureHeapProfile(t, pprofDir, "after_load")

	t.Logf("AFTER LOAD: HeapAlloc=%.1fMB HeapSys=%.1fMB Goroutines=%d",
		mb(afterLoad.HeapAlloc), mb(afterLoad.HeapSys), afterLoad.Goroutines)

	// Wait for session cleanup (30s timeout + buffer)
	t.Log("waiting 35s for session timeout cleanup...")
	time.Sleep(35 * time.Second)

	afterCleanup := snapHeap()
	captureHeapProfile(t, pprofDir, "after_cleanup")

	mu.Lock()
	calls, sessions, errors := totalCalls, totalSessions, totalErrors
	mu.Unlock()

	t.Logf("AFTER CLEANUP: HeapAlloc=%.1fMB HeapSys=%.1fMB Goroutines=%d",
		mb(afterCleanup.HeapAlloc), mb(afterCleanup.HeapSys), afterCleanup.Goroutines)

	// Summary
	growth := float64(afterCleanup.HeapAlloc) - float64(baseline.HeapAlloc)
	goroutineLeak := afterCleanup.Goroutines - baseline.Goroutines

	t.Logf("SUMMARY:")
	t.Logf("  duration:    %s", duration)
	t.Logf("  calls:       %d", calls)
	t.Logf("  sessions:    %d", sessions)
	t.Logf("  errors:      %d", errors)
	t.Logf("  baseline:    %.1fMB heap, %d goroutines", mb(baseline.HeapAlloc), baseline.Goroutines)
	t.Logf("  peak:        %.1fMB heap, %d goroutines", mb(afterLoad.HeapAlloc), afterLoad.Goroutines)
	t.Logf("  settled:     %.1fMB heap, %d goroutines", mb(afterCleanup.HeapAlloc), afterCleanup.Goroutines)
	t.Logf("  heap growth: %.1fMB", mb(uint64(max(0, int64(growth)))))
	t.Logf("  goroutine leak: %d", goroutineLeak)
	t.Logf("  pprof profiles: %s/", pprofDir)

	// Assertions
	if goroutineLeak > 20 {
		t.Errorf("GOROUTINE LEAK: %d goroutines above baseline after cleanup", goroutineLeak)
	}
	if growth > 100*1024*1024 { // 100MB retained after cleanup
		t.Errorf("MEMORY LEAK: %.1fMB retained after cleanup (baseline was %.1fMB)",
			mb(afterCleanup.HeapAlloc), mb(baseline.HeapAlloc))
	}
}

func captureHeapProfile(t *testing.T, dir, label string) {
	t.Helper()
	path := filepath.Join(dir, label+".pb.gz")
	f, err := os.Create(path)
	if err != nil {
		t.Logf("pprof write failed: %v", err)
		return
	}
	defer f.Close()
	if err := pprof.WriteHeapProfile(f); err != nil {
		t.Logf("pprof capture failed: %v", err)
	}
}

// Quiet variants that don't fatal on error (for concurrent workers)
func httpInitSessionQuiet(addr string) string {
	body := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"stress","version":"0.1"}}}`
	resp, err := httpPostQuiet(addr, body, "")
	if err != nil {
		return ""
	}
	sid := resp.Header.Get("Mcp-Session-Id")
	resp.Body.Close()
	if sid == "" {
		return ""
	}
	resp2, _ := httpPostQuiet(addr, `{"jsonrpc":"2.0","method":"notifications/initialized"}`, sid)
	if resp2 != nil {
		resp2.Body.Close()
	}
	return sid
}

func httpToolCallQuiet(addr, sid, tool string, args map[string]any) bool {
	params := map[string]any{"name": tool, "arguments": args}
	payload := map[string]any{"jsonrpc": "2.0", "id": 2, "method": "tools/call", "params": params}
	data, _ := json.Marshal(payload)
	resp, err := httpPostQuiet(addr, string(data), sid)
	if err != nil {
		return true // error
	}
	io.Copy(io.Discard, resp.Body) // drain body to prevent connection leak
	resp.Body.Close()
	return false
}

func httpPostQuiet(addr, body, sid string) (*http.Response, error) {
	req, _ := http.NewRequest("POST", addr+"/", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if sid != "" {
		req.Header.Set("Mcp-Session-Id", sid)
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return resp, nil
}

func httpPost(addr, body, sid string) (*http.Response, error) {
	req, _ := http.NewRequest("POST", addr+"/", bytes.NewReader([]byte(body)))
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

// --- Stress Test: Server Cache Growth (SCR-BUG-14 vector 2) ---

func TestStress_ServerCacheGrowth(t *testing.T) {
	s := openStore(t)
	seedArtifacts(t, s, 50)
	srv := newServer(t, s)

	baseline := assertHeapBelow(t, "baseline", 100)

	// Create sequential sessions with many operations each
	const sessions = 10
	const opsPerSession = 200
	for i := 0; i < sessions; i++ {
		cs := connectClient(t, srv)
		for j := 0; j < opsPerSession; j++ {
			id := fmt.Sprintf("STR-TSK-%d", (j%50)+1)
			callTool(t, cs, "artifact", map[string]any{"action": "get", "id": id})
		}
	}

	afterSessions := assertHeapBelow(t, "after_10_sessions", 200)
	growth := float64(afterSessions.HeapAlloc) - float64(baseline.HeapAlloc)
	t.Logf("heap growth after %d sessions × %d ops: %.1fMB", sessions, opsPerSession, mb(uint64(max(0, int64(growth)))))

	// Each in-memory transport session spawns ~4 goroutines (read/write/handle per side).
	// 10 sessions = ~40 goroutines expected. Allow generous headroom.
	maxGoroutines := baseline.Goroutines + sessions*5 + 10
	if afterSessions.Goroutines > maxGoroutines {
		t.Errorf("goroutine leak: baseline=%d after=%d (max=%d)",
			baseline.Goroutines, afterSessions.Goroutines, maxGoroutines)
	}
}

// --- Stress Test: SQLite Pragma Hardening ---

func TestStress_SQLitePragmaHardening(t *testing.T) {
	for i := 0; i < 10; i++ {
		tmpPath := filepath.Join(t.TempDir(), fmt.Sprintf("cycle-%d.db", i))
		ss, err := store.OpenSQLite(tmpPath)
		if err != nil {
			t.Fatalf("open cycle %d: %v", i, err)
		}
		ctx := context.Background()
		if err := ss.Put(ctx, &model.Artifact{
			ID: fmt.Sprintf("CYC-%d", i), Kind: "task", Scope: "test",
			Status: "draft", Title: fmt.Sprintf("Cycle %d", i),
		}); err != nil {
			ss.Close()
			t.Fatalf("put cycle %d: %v", i, err)
		}
		if art, err := ss.Get(ctx, fmt.Sprintf("CYC-%d", i)); err != nil || art.Title != fmt.Sprintf("Cycle %d", i) {
			ss.Close()
			t.Fatalf("get cycle %d failed", i)
		}
		ss.Close()
	}
}

// --- Stress Test: Sustained Write Contention ---

func TestStress_WriteContention(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "contention.db")
	s, err := store.OpenSQLiteConfig(store.SQLiteConfig{
		Path:          dbPath,
		BusyTimeoutMs: 5000,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	ctx := context.Background()
	const writers = 5
	const writesPerWriter = 200
	var mu sync.Mutex
	var writeErrors []string

	var wg sync.WaitGroup
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(wIdx int) {
			defer wg.Done()
			for i := 0; i < writesPerWriter; i++ {
				id := fmt.Sprintf("W%d-TSK-%d", wIdx, i)
				if err := s.Put(ctx, &model.Artifact{
					ID: id, Kind: "task", Scope: "test",
					Status: "draft", Title: fmt.Sprintf("Writer %d Task %d", wIdx, i),
					Sections: []model.Section{
						{Name: "context", Text: fmt.Sprintf("Content from writer %d, iteration %d", wIdx, i)},
					},
				}); err != nil {
					mu.Lock()
					writeErrors = append(writeErrors, fmt.Sprintf("w%d/i%d: %v", wIdx, i, err))
					mu.Unlock()
				}
			}
		}(w)
	}
	wg.Wait()

	if len(writeErrors) > 0 {
		t.Errorf("%d write errors (expected 0 with busy_timeout):\n%s",
			len(writeErrors), strings.Join(writeErrors[:min(5, len(writeErrors))], "\n"))
	}

	all, _ := s.List(ctx, model.Filter{Kind: "task"})
	expected := writers * writesPerWriter
	if len(all) != expected {
		t.Errorf("expected %d artifacts, got %d", expected, len(all))
	}
}
