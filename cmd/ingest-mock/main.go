// ingest-mock streams a synthetic Locus-shaped NDJSON payload to stdout.
// Pipe it into `scribe ingest` or curl to stress-test POST /api/v1/ingest.
//
// Usage:
//
//	ingest-mock | curl -s -X POST http://localhost:8083/api/v1/ingest \
//	    -H 'Content-Type: application/x-ndjson' --data-binary @- | jq
//
//	ingest-mock --components 50 --symbols 20 --rate 1000
//
//nolint:gosec // mock/test tool — weak rand is intentional for synthetic data generation
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"math/rand/v2"
	"os"
	"time"
)

func main() { //nolint:funlen,gosec,staticcheck // mock tool: length intentional; weak rand fine for synthetic data
	components := flag.Int("components", 20, "number of component nodes")
	symbolsPerComp := flag.Int("symbols", 10, "symbols per component")
	edgesPerComp := flag.Int("edges", 5, "cross-component edges per component")
	rate := flag.Int("rate", 0, "records/s (0 = unlimited)")
	scanSHA := flag.String("sha", "deadbeef", "scan SHA embedded in meta record")
	flag.Parse()

	enc := json.NewEncoder(os.Stdout)
	enc.SetEscapeHTML(false)

	var throttle <-chan time.Time
	if *rate > 0 {
		throttle = time.Tick(time.Second / time.Duration(*rate))
	}

	emit := func(v any) {
		if throttle != nil {
			<-throttle
		}
		if err := enc.Encode(v); err != nil {
			fmt.Fprintf(os.Stderr, "encode error: %v\n", err)
			os.Exit(1)
		}
	}

	totalNodes := 0
	totalEdges := 0

	// ── Components ────────────────────────────────────────────────────────
	compIDs := make([]string, *components)
	for i := range *components {
		id := fmt.Sprintf("locus:component:pkg/comp%d", i)
		compIDs[i] = id
		fanIn := rand.IntN(20)
		fanOut := rand.IntN(30)
		emit(map[string]any{
			"type":   "node",
			"id":     id,
			"kind":   "note",
			"title":  fmt.Sprintf("pkg/comp%d", i),
			"status": "active",
			"labels": []string{
				"source:locus",
				"kind:component",
				"lang:go",
			},
			"extra": map[string]any{
				"fan_in":     fanIn,
				"fan_out":    fanOut,
				"churn":      rand.IntN(50),
				"loc":        rand.IntN(2000) + 100,
				"scan_sha":   *scanSHA,
				"scanned_at": time.Now().UTC().Format(time.RFC3339),
			},
		})
		totalNodes++

		// ── Symbols inside this component ─────────────────────────────
		for j := range *symbolsPerComp {
			symID := fmt.Sprintf("locus:symbol:comp%d/file.go:Func%d", i, j)
			emit(map[string]any{
				"type":   "node",
				"id":     symID,
				"kind":   "note",
				"title":  fmt.Sprintf("Func%d", j),
				"status": "active",
				"labels": []string{
					"source:locus",
					"kind:symbol",
					fmt.Sprintf("file:comp%d/file.go", i),
					"exported:true",
				},
				"extra": map[string]any{
					"fan_in":   rand.IntN(10),
					"fan_out":  rand.IntN(8),
					"scan_sha": *scanSHA,
				},
			})
			totalNodes++

			// symbol belongs_to its component
			emit(map[string]any{
				"type":     "edge",
				"from":     symID,
				"to":       id,
				"relation": "belongs_to",
			})
			totalEdges++
		}
	}

	// ── Cross-component import edges ───────────────────────────────────────
	for i, from := range compIDs {
		for range *edgesPerComp {
			to := compIDs[rand.IntN(len(compIDs))]
			if to == from {
				continue
			}
			emit(map[string]any{
				"type":     "edge",
				"from":     fmt.Sprintf("locus:symbol:comp%d/file.go:Func%d", i, rand.IntN(*symbolsPerComp)),
				"to":       to,
				"relation": "imports",
				"weight":   rand.Float64(),
			})
			totalEdges++
		}
	}

	// ── Violations (cycles) ────────────────────────────────────────────────
	if *components > 2 {
		for range max(1, *components/10) {
			a := rand.IntN(*components)
			b := rand.IntN(*components)
			if a == b {
				continue
			}
			id := fmt.Sprintf("locus:violation:cycle:comp%d-comp%d", a, b)
			emit(map[string]any{
				"type":   "node",
				"id":     id,
				"kind":   "bug",
				"title":  fmt.Sprintf("import cycle: comp%d ↔ comp%d", a, b),
				"status": "active",
				"labels": []string{"source:locus", "kind:violation", "violation:cycle"},
				"extra":  map[string]any{"scan_sha": *scanSHA},
			})
			totalNodes++
		}
	}

	// ── Meta record (end-of-stream marker) ────────────────────────────────
	emit(map[string]any{
		"type":        "meta",
		"source":      "locus",
		"scan_sha":    *scanSHA,
		"scanned_at":  time.Now().UTC().Format(time.RFC3339),
		"total_nodes": totalNodes,
		"total_edges": totalEdges,
	})

	fmt.Fprintf(os.Stderr, "streamed %d nodes, %d edges\n", totalNodes, totalEdges)
}
