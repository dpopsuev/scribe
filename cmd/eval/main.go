// eval — retrieval evaluation harness for agent memory A/B testing.
//
// Measures Precision@5 for FTS5 vs cosine search across a corpus of
// real session compaction summaries.
//
// Usage:
//
//	eval ingest --sessions <dir>           ingest compactions into knowledge store
//	eval run    --queries <yaml>           run queries, print results for scoring
//	eval score  --results <json>           interactive scoring of a result set
//	eval report                            precision@5 across all scored result sets
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	parchment "github.com/dpopsuev/parchment"
	"github.com/dpopsuev/scribe/mcp"
	"gopkg.in/yaml.v3"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}
	switch os.Args[1] {
	case "ingest":
		runIngest(os.Args[2:])
	case "run":
		runQueries(os.Args[2:])
	case "score":
		runScore(os.Args[2:])
	case "report":
		runReport(os.Args[2:])
	default:
		usage()
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `eval — retrieval evaluation harness

Commands:
  ingest  --sessions <dir> [--db <path>]   ingest compactions into knowledge store
  run     --queries <yaml> [--db <path>]   run queries and print results
  score   --results <json>                  interactively score results
  report  [--results <dir>]                 show precision@5 summary`)
}

// --- ingest ---

func runIngest(args []string) {
	sessionsDir, dbPath := "", defaultDB()
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--sessions":
			i++
			sessionsDir = args[i]
		case "--db":
			i++
			dbPath = args[i]
		}
	}
	if sessionsDir == "" {
		sessionsDir = filepath.Join(os.Getenv("HOME"), ".claude", "projects")
	}

	s, err := parchment.OpenSQLite(dbPath)
	if err != nil {
		fatal("open db: %v", err)
	}
	defer func() { _ = s.Close() }()

	proto := parchment.New(s, parchment.KnowledgeSchema(), []string{"eval"}, nil, parchment.ProtocolConfig{})
	ctx := context.Background()

	// Collect all JSONL files recursively.
	var paths []string
	_ = filepath.Walk(sessionsDir, func(p string, info os.FileInfo, err error) error { //nolint:gosec // eval tool reads operator-provided session directories
		if err != nil || info.IsDir() {
			return nil
		}
		if strings.HasSuffix(p, ".jsonl") && !strings.Contains(p, "subagents") {
			paths = append(paths, p)
		}
		return nil
	})

	fmt.Printf("Found %d session files. Ingesting...\n", len(paths))
	created, skipped, errors := 0, 0, 0
	for _, p := range paths {
		n, s, err := ingestOne(ctx, proto, p)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  SKIP %s: %v\n", filepath.Base(p), err)
			errors++
			continue
		}
		created += n
		skipped += s
	}
	fmt.Printf("Done: %d created, %d skipped (already indexed), %d errors\n", created, skipped, errors)
}

func ingestOne(ctx context.Context, proto *parchment.Protocol, path string) (created, skipped int, err error) {
	// Check idempotency.
	existing, _ := proto.ListArtifacts(ctx, parchment.ListInput{Kind: parchment.KindSource, Scope: "eval"})
	for _, s := range existing {
		for _, sec := range s.Sections {
			if sec.Name == "provenance" && strings.Contains(sec.Text, path) {
				return 0, 1, nil
			}
		}
	}

	// Parse compactions from the session file.
	f, err := os.Open(path) //nolint:gosec // eval tool — operator-provided session path
	if err != nil {
		return 0, 0, err
	}
	defer func() { _ = f.Close() }()

	var compactions []string
	proj := projectFromPath(path)
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		var entry map[string]json.RawMessage
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			continue
		}
		switch jsonStr(entry["type"]) {
		case "compaction", "branch_summary":
			if s := jsonStr(entry["summary"]); len(s) > 50 {
				compactions = append(compactions, s)
			}
		case "user":
			// Claude Code compaction: user message containing rich summary.
			var msg struct {
				Content string `json:"content"`
			}
			if err := json.Unmarshal(entry["message"], &msg); err == nil {
				if strings.Contains(msg.Content, "Summary:") && len(msg.Content) > 200 {
					start := strings.Index(msg.Content, "Summary:")
					compactions = append(compactions, msg.Content[start:start+minInt(1500, len(msg.Content)-start)])
				}
			}
		}
	}

	if len(compactions) == 0 {
		return 0, 0, nil // no compactions, skip silently
	}

	// Create source artifact for the session.
	filename := filepath.Base(path)
	title := fmt.Sprintf("Session: %s [%s]", filename, proj)
	src, err := proto.CreateArtifact(ctx, parchment.CreateInput{
		Kind:  parchment.KindSource,
		Title: title,
		Scope: "eval",
		Sections: []parchment.Section{
			{Name: "provenance", Text: path},
			{Name: "project", Text: proj},
		},
	})
	if err != nil {
		return 0, 0, fmt.Errorf("create source: %w", err)
	}
	created++

	// Create context artifact per compaction.
	for i, c := range compactions {
		title := truncate(firstLine(c), 80)
		if title == "" {
			title = fmt.Sprintf("Session memory %d", i+1)
		}
		art, err := proto.CreateArtifact(ctx, parchment.CreateInput{
			Kind:  parchment.KindContext,
			Title: title,
			Scope: "eval",
			Sections: []parchment.Section{
				{Name: "body", Text: c},
				{Name: "project", Text: proj},
			},
		})
		if err != nil {
			continue
		}
		_, _ = proto.LinkArtifacts(ctx, art.ID, parchment.RelCites, []string{src.ID})
		created++
	}
	return created, 0, nil
}

// --- run ---

type Query struct {
	ID            string `yaml:"id"`
	Band          string `yaml:"band"`
	Text          string `yaml:"text"`
	ExpectedTheme string `yaml:"expected_theme"`
}

type QueryFile struct {
	Queries []Query `yaml:"queries"`
}

type Result struct {
	RunAt   time.Time     `json:"run_at"`
	System  string        `json:"system"`
	Queries []QueryResult `json:"queries"`
}

type QueryResult struct {
	Query  Query `json:"query"`
	Hits   []Hit `json:"hits"`
	Scores []int `json:"scores,omitempty"` // 0 or 1 per hit, set during scoring
}

type Hit struct {
	ID      string `json:"id"`
	Kind    string `json:"kind"`
	Status  string `json:"status"`
	Title   string `json:"title"`
	Excerpt string `json:"excerpt"`
}

func runQueries(args []string) {
	queriesFile, dbPath, system, outDir := "eval/queries.yaml", defaultDB(), "A", "eval/results"
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--queries":
			i++
			queriesFile = args[i]
		case "--db":
			i++
			dbPath = args[i]
		case "--system":
			i++
			system = args[i]
		case "--out":
			i++
			outDir = args[i]
		}
	}

	data, err := os.ReadFile(queriesFile) //nolint:gosec // eval tool reads operator-provided queries file
	if err != nil {
		fatal("read queries: %v", err)
	}
	var qf QueryFile
	if err := yaml.Unmarshal(data, &qf); err != nil {
		fatal("parse queries: %v", err)
	}

	s, err := parchment.OpenSQLite(dbPath)
	if err != nil {
		fatal("open db: %v", err)
	}
	proto := parchment.New(s, parchment.KnowledgeSchema(), []string{"eval"}, nil, parchment.ProtocolConfig{})
	ctx := context.Background()

	// Check corpus size.
	all, _ := proto.ListArtifacts(ctx, parchment.ListInput{Scope: "eval"})
	fmt.Printf("Corpus: %d artifacts in store (scope=eval)\n", len(all))
	if len(all) == 0 {
		_ = s.Close()
		fmt.Println("Empty corpus — run: eval ingest --sessions <dir>")
		os.Exit(1)
	}
	defer func() { _ = s.Close() }()

	result := Result{RunAt: time.Now(), System: system}

	for _, q := range qf.Queries {
		fmt.Printf("\n[%s|%s] %s\n", q.ID, q.Band, q.Text)
		fmt.Printf("  theme: %s\n", q.ExpectedTheme)

		hits := recall(ctx, proto, q.Text, 5)
		qr := QueryResult{Query: q, Hits: hits}
		result.Queries = append(result.Queries, qr)

		for i, h := range hits {
			fmt.Printf("  %d. [%s] %s\n     %s\n", i+1, h.Kind, h.Title, h.Excerpt)
		}
		if len(hits) == 0 {
			fmt.Println("  (no results)")
		}
	}

	// Save results.
	if err := os.MkdirAll(outDir, 0o750); err != nil { //nolint:gosec // eval tool output dir is operator-controlled
		fatal("mkdir: %v", err)
	}
	outPath := filepath.Join(outDir, fmt.Sprintf("%s-%s.json", system, time.Now().Format("20060102-150405")))
	out, _ := json.MarshalIndent(result, "", "  ")
	if err := os.WriteFile(outPath, out, 0o600); err != nil { //nolint:gosec // eval output file, operator-controlled path
		fatal("write results: %v", err)
	}
	fmt.Printf("\nResults saved to %s\n", outPath)
	fmt.Printf("Score with: eval score --results %s\n", outPath)
}

// recall runs the retrieval query against the store and returns top-n hits.
// System A uses FTS5 via SearchArtifacts. System B will use cosine (future).
func recall(ctx context.Context, proto *parchment.Protocol, query string, n int) []Hit {
	knowledgeKinds := map[string]bool{
		parchment.KindNote:    true,
		parchment.KindJournal: true,
		parchment.KindSource:  true,
		parchment.KindConcept: true,
		parchment.KindContext: true,
	}

	// Multi-pass FTS.
	seen := map[string]bool{}
	var candidates []*parchment.Artifact

	passes := buildPasses(query)
	for _, q := range passes {
		arts, err := proto.SearchArtifacts(ctx, q, parchment.ListInput{Scope: "eval"})
		if err != nil {
			continue
		}
		for _, a := range arts {
			if seen[a.ID] || !knowledgeKinds[a.Kind] {
				continue
			}
			seen[a.ID] = true
			candidates = append(candidates, a)
		}
		if len(candidates) >= 20 {
			break
		}
	}

	// Sort by recency.
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].UpdatedAt.After(candidates[j].UpdatedAt)
	})

	limit := n
	if len(candidates) < limit {
		limit = len(candidates)
	}

	hits := make([]Hit, 0, n)
	for _, a := range candidates[:limit] {
		excerpt := extractExcerpt(a, strings.Fields(strings.ToLower(query)))
		hits = append(hits, Hit{
			ID:      a.ID,
			Kind:    a.Kind,
			Status:  a.Status,
			Title:   a.Title,
			Excerpt: excerpt,
		})
	}
	return hits
}

// --- score ---

func runScore(args []string) {
	resultsPath := ""
	for i := 0; i < len(args); i++ {
		if args[i] == "--results" {
			i++
			resultsPath = args[i]
		}
	}
	if resultsPath == "" {
		// Use latest.
		entries, _ := filepath.Glob("eval/results/*.json")
		if len(entries) == 0 {
			fatal("no result files found in eval/results/")
		}
		sort.Strings(entries)
		resultsPath = entries[len(entries)-1]
	}

	data, err := os.ReadFile(resultsPath) //nolint:gosec // eval tool reads operator-provided results file
	if err != nil {
		fatal("read results: %v", err)
	}
	var result Result
	if err := json.Unmarshal(data, &result); err != nil {
		fatal("parse results: %v", err)
	}

	fmt.Printf("Scoring system %s (%s)\n", result.System, result.RunAt.Format("2006-01-02 15:04"))
	fmt.Println("For each result: enter 1 (relevant) or 0 (not relevant), then Enter.")
	fmt.Println("Press Ctrl-C to stop and save partial scores.")

	reader := bufio.NewReader(os.Stdin)
	for qi := range result.Queries {
		qr := &result.Queries[qi]
		if len(qr.Scores) >= len(qr.Hits) {
			continue // already scored
		}
		fmt.Printf("\n[%s|%s] %s\n", qr.Query.ID, qr.Query.Band, qr.Query.Text)
		fmt.Printf("  Expected: %s\n", qr.Query.ExpectedTheme)

		scores := make([]int, len(qr.Hits))
		for i, h := range qr.Hits {
			fmt.Printf("  %d. [%s] %s\n     %s\n  Score [0/1]: ", i+1, h.Kind, h.Title, h.Excerpt)
			line, _ := reader.ReadString('\n')
			line = strings.TrimSpace(line)
			if line == "1" {
				scores[i] = 1
			}
		}
		qr.Scores = scores

		p5 := precision5(scores)
		fmt.Printf("  → Precision@5: %.2f\n", p5)
	}

	out, _ := json.MarshalIndent(result, "", "  ")
	if err := os.WriteFile(resultsPath, out, 0o600); err != nil { //nolint:gosec // eval scores file, operator-controlled path
		fatal("save scores: %v", err)
	}
	fmt.Printf("\nScores saved to %s\n", resultsPath)
	fmt.Printf("Run: eval report to see summary\n")
}

// --- report ---

type sysResult struct {
	system  string
	byBand  map[string][]float64
	overall []float64
}

func runReport(args []string) {
	resultsDir := "eval/results"
	for i := 0; i < len(args); i++ {
		if args[i] == "--results" {
			i++
			resultsDir = args[i]
		}
	}

	entries, _ := filepath.Glob(filepath.Join(resultsDir, "*.json"))
	if len(entries) == 0 {
		fmt.Println("No result files found.")
		return
	}

	systems := map[string]*sysResult{}
	for _, path := range entries {
		data, err := os.ReadFile(path) //nolint:gosec // eval tool reads operator-controlled result files
		if err != nil {
			continue
		}
		var result Result
		if err := json.Unmarshal(data, &result); err != nil {
			continue
		}
		if _, ok := systems[result.System]; !ok {
			systems[result.System] = &sysResult{
				system: result.System,
				byBand: map[string][]float64{},
			}
		}
		sr := systems[result.System]
		for _, qr := range result.Queries {
			if len(qr.Scores) == 0 {
				continue
			}
			p5 := precision5(qr.Scores)
			sr.overall = append(sr.overall, p5)
			sr.byBand[qr.Query.Band] = append(sr.byBand[qr.Query.Band], p5)
		}
	}

	if len(systems) == 0 {
		fmt.Println("No scored results found. Run: eval score --results <file>")
		return
	}

	bands := []string{"easy", "medium", "hard", "cross"}
	fmt.Printf("%-8s  %-8s  %-8s  %-8s  %-8s  %s\n", "System", "Easy", "Medium", "Hard", "Cross", "Overall")
	fmt.Println(strings.Repeat("-", 60))

	for _, sys := range sortedSystems(systems) {
		sr := systems[sys]
		cols := []string{sys}
		for _, b := range bands {
			cols = append(cols, fmtMean(sr.byBand[b]))
		}
		cols = append(cols, fmtMean(sr.overall))
		fmt.Printf("%-8s  %-8s  %-8s  %-8s  %-8s  %s\n", cols[0], cols[1], cols[2], cols[3], cols[4], cols[5])
	}
}

// --- helpers ---

func buildPasses(query string) []string {
	words := strings.Fields(query)
	passes := []string{`"` + strings.Join(words, " ") + `"`}
	if len(words) > 1 {
		passes = append(passes, strings.Join(words, " "))
	}
	for _, w := range words {
		if len(w) >= 4 {
			passes = append(passes, w)
		}
	}
	return passes
}

func extractExcerpt(art *parchment.Artifact, terms []string) string {
	for _, sec := range art.Sections {
		lower := strings.ToLower(sec.Text)
		for _, t := range terms {
			idx := strings.Index(lower, t)
			if idx < 0 {
				continue
			}
			start := idx - 40
			if start < 0 {
				start = 0
			}
			end := idx + 80
			if end > len(sec.Text) {
				end = len(sec.Text)
			}
			excerpt := strings.TrimSpace(sec.Text[start:end])
			if len(excerpt) > 120 {
				excerpt = excerpt[:117] + "…"
			}
			return excerpt
		}
	}
	return ""
}

func precision5(scores []int) float64 {
	if len(scores) == 0 {
		return 0
	}
	sum := 0
	for _, s := range scores {
		sum += s
	}
	denom := 5.0
	if float64(len(scores)) < denom {
		denom = float64(len(scores))
	}
	return float64(sum) / denom
}

func fmtMean(vals []float64) string {
	if len(vals) == 0 {
		return "—"
	}
	sum := 0.0
	for _, v := range vals {
		sum += v
	}
	return fmt.Sprintf("%.2f", sum/float64(len(vals)))
}

func sortedSystems(m map[string]*sysResult) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func projectFromPath(path string) string {
	parts := strings.Split(path, "/")
	for i, p := range parts {
		if p == "projects" && i+1 < len(parts) {
			return strings.ReplaceAll(parts[i+1], "-home-dpopsuev-Workspace-", "")
		}
		if p == "sessions" && i+1 < len(parts) {
			return strings.ReplaceAll(parts[i+1], "--home-dpopsuev-Workspace-", "pi:")
		}
	}
	return filepath.Base(filepath.Dir(path))
}

// firstLine returns the first meaningful line from a compaction summary,
// skipping structural headers like "1. Primary Request and Intent:".
func firstLine(s string) string {
	skip := map[string]bool{
		"summary:": true, "primary request and intent:": true,
		"primary request:": true,
	}
	for _, l := range strings.Split(s, "\n") {
		l = strings.TrimSpace(l)
		if len(l) < 15 {
			continue
		}
		lower := strings.ToLower(l)
		if skip[lower] {
			continue
		}
		if l != "" && l[0] >= '1' && l[0] <= '9' &&
			strings.HasPrefix(l[1:], ". ") && strings.HasSuffix(l, ":") {
			continue
		}
		return l
	}
	return ""
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

func jsonStr(raw json.RawMessage) string {
	if raw == nil {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return ""
	}
	return s
}

func defaultDB() string {
	if v := os.Getenv("SCRIBE_ROOT"); v != "" {
		return filepath.Join(v, "scribe.sqlite")
	}
	return filepath.Join(os.Getenv("HOME"), ".scribe", "scribe.sqlite")
}

func fatal(msg string, args ...any) {
	fmt.Fprintf(os.Stderr, "eval: "+msg+"\n", args...)
	os.Exit(1)
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// Ensure mcp import is used (for future System B wiring).
var _ = mcp.IsComponentLabel
var _ io.Reader // ensure io is used
