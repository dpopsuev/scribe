package mcp

// ingest_session.go — knowledge(action=ingest_session) implementation.
//
// Imports Pi (v3) and Claude Code JSONL session files into the knowledge vault.
// Extracts compaction summaries as context artifacts. Creates one source artifact
// per session file with a provenance section. Idempotent: skips sessions already
// indexed by matching the provenance path.

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	parchment "github.com/dpopsuev/parchment"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// handleIngestSession is the handler for knowledge(action=ingest_session).
func (h *handler) handleIngestSession(ctx context.Context, in knowledgeInput) (*sdkmcp.CallToolResult, any, error) {
	if in.Path == "" {
		return text("path is required for ingest_session — pass a .jsonl file or a directory"), nil, nil
	}

	fi, err := os.Stat(in.Path)
	if err != nil {
		return text(fmt.Sprintf("cannot access path %q: %v", in.Path, err)), nil, nil
	}

	var paths []string
	if fi.IsDir() {
		entries, err := os.ReadDir(in.Path)
		if err != nil {
			return text(fmt.Sprintf("cannot read directory: %v", err)), nil, nil
		}
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".jsonl") {
				paths = append(paths, filepath.Join(in.Path, e.Name()))
			}
		}
	} else {
		paths = []string{in.Path}
	}

	if len(paths) == 0 {
		return text("no .jsonl files found at " + in.Path), nil, nil
	}

	scope := in.Scope
	if scope == "" && len(h.homeScopes) > 0 {
		scope = h.homeScopes[0]
	}

	var created, skipped int
	var b strings.Builder

	for _, p := range paths {
		n, s, err := h.ingestSessionFile(ctx, p, scope)
		if err != nil {
			fmt.Fprintf(&b, "  error ingesting %s: %v\n", filepath.Base(p), err)
			continue
		}
		created += n
		skipped += s
		if n > 0 {
			fmt.Fprintf(&b, "  %s → %d artifact(s) created\n", filepath.Base(p), n)
		} else {
			fmt.Fprintf(&b, "  %s → already indexed (skipped)\n", filepath.Base(p))
		}
	}

	fmt.Fprintf(&b, "\nIngested %d session(s): %d artifact(s) created, %d skipped (already indexed).\n", len(paths), created, skipped)
	if created > 0 {
		fmt.Fprintf(&b, "\nNext — you are the compiler:\n")
		fmt.Fprintf(&b, "  knowledge(action=catalog, scope=%s) — browse what was extracted\n", scope)
		fmt.Fprintf(&b, "  knowledge(action=synthesize, query=<topic>) — connect related notes\n")
	}
	return text(b.String()), nil, nil
}

// ingestSessionFile parses one JSONL file and creates knowledge artifacts.
// Returns (created, skipped, error). skipped=1 means this session was already indexed.
func (h *handler) ingestSessionFile(ctx context.Context, path, scope string) (created, skipped int, err error) {
	// Idempotency: check if we already have a source with this provenance.
	existing, _ := h.proto.ListArtifacts(ctx, parchment.ListInput{Kind: parchment.KindSource, Scope: scope})
	for _, s := range existing {
		for _, sec := range s.Sections {
			if sec.Name == "provenance" && strings.Contains(sec.Text, path) {
				return 0, 1, nil
			}
		}
	}

	sess, err := parseSessionJSONL(path)
	if err != nil {
		return 0, 0, err
	}

	// Create the session source artifact.
	title := fmt.Sprintf("Session: %s", filepath.Base(path))
	if sess.cwd != "" {
		title = fmt.Sprintf("Session: %s (%s)", filepath.Base(path), filepath.Base(sess.cwd))
	}
	source, err := h.proto.CreateArtifact(ctx, parchment.CreateInput{
		Kind:  parchment.KindSource,
		Title: title,
		Scope: scope,
		Sections: []parchment.Section{
			{Name: "provenance", Text: path},
			{Name: "summary", Text: sess.summary()},
		},
	})
	if err != nil {
		return 0, 0, fmt.Errorf("create source: %w", err)
	}
	created++

	// Extract compaction summaries as context artifacts.
	for _, c := range sess.compactions {
		if c == "" {
			continue
		}
		art, err := h.proto.CreateArtifact(ctx, parchment.CreateInput{
			Kind:  parchment.KindContext,
			Title: extractTitle(c),
			Scope: scope,
			Sections: []parchment.Section{
				{Name: "summary", Text: c},
			},
		})
		if err != nil {
			continue
		}
		_, _ = h.proto.LinkArtifacts(ctx, art.ID, parchment.RelCites, []string{source.ID})
		created++
	}

	return created, 0, nil
}

// sessionData holds the parsed content of a JSONL session file.
type sessionData struct {
	format      string   // "pi" or "claude"
	cwd         string   // working directory (Pi only)
	compactions []string // compaction / branch summaries
	toolCalls   int      // total scribe tool calls observed
}

func (s *sessionData) summary() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Format: %s\n", s.format)
	if s.cwd != "" {
		fmt.Fprintf(&b, "CWD: %s\n", s.cwd)
	}
	fmt.Fprintf(&b, "Compaction summaries: %d\n", len(s.compactions))
	fmt.Fprintf(&b, "Scribe tool calls observed: %d\n", s.toolCalls)
	return b.String()
}

// parseSessionJSONL reads a JSONL file and detects whether it is a Pi v3
// session or a Claude Code session, then extracts structured content.
func parseSessionJSONL(path string) (*sessionData, error) {
	f, err := os.Open(path) //nolint:gosec // path comes from operator-controlled config, not user input
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	sess := &sessionData{}
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var entry map[string]json.RawMessage
		if err := json.Unmarshal(line, &entry); err != nil {
			continue
		}

		entryType := jsonString(entry["type"])

		switch entryType {
		// Pi v3 session header
		case "session":
			sess.format = "pi"
			sess.cwd = jsonString(entry["cwd"])

		// Pi v3 compaction
		case "compaction":
			if s := jsonString(entry["summary"]); s != "" {
				sess.compactions = append(sess.compactions, s)
			}

		// Pi v3 branch summary
		case "branch_summary":
			if s := jsonString(entry["summary"]); s != "" {
				sess.compactions = append(sess.compactions, s)
			}

		// Pi v3 message — look for scribe tool calls
		case "message":
			sess.toolCalls += countScribeToolCallsPi(entry["message"])

		// Claude Code assistant message — look for scribe tool calls
		case "assistant":
			if sess.format == "" {
				sess.format = "claude"
			}
			sess.toolCalls += countScribeToolCallsClaude(entry["message"])

		// Claude Code permission-mode / file-history — just detect format
		case "permission-mode", "file-history-snapshot":
			if sess.format == "" {
				sess.format = "claude"
			}
		}
	}

	if sess.format == "" {
		sess.format = "unknown"
	}
	return sess, scanner.Err()
}

// countScribeToolCallsPi counts scribe tool calls in a Pi message object.
func countScribeToolCallsPi(raw json.RawMessage) int {
	if raw == nil {
		return 0
	}
	var msg struct {
		Content []struct {
			Type string `json:"type"`
			Name string `json:"name"`
		} `json:"content"`
	}
	if err := json.Unmarshal(raw, &msg); err != nil {
		return 0
	}
	n := 0
	for _, block := range msg.Content {
		if block.Type == "toolCall" && strings.HasPrefix(block.Name, "scribe_") {
			n++
		}
	}
	return n
}

// countScribeToolCallsClaude counts scribe tool calls in a Claude Code message object.
func countScribeToolCallsClaude(raw json.RawMessage) int {
	if raw == nil {
		return 0
	}
	var msg struct {
		Content []struct {
			Type string `json:"type"`
			Name string `json:"name"`
		} `json:"content"`
	}
	if err := json.Unmarshal(raw, &msg); err != nil {
		return 0
	}
	n := 0
	for _, block := range msg.Content {
		if block.Type == "tool_use" && strings.Contains(block.Name, "scribe") {
			n++
		}
	}
	return n
}

// jsonString safely extracts a string from a raw JSON value.
func jsonString(raw json.RawMessage) string {
	if raw == nil {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return ""
	}
	return s
}

// extractTitle returns a meaningful title from a compaction summary.
// Claude Code compactions start with structural headers like
// "1. Primary Request and Intent:" that are useless as titles.
// Pi compactions start with the actual goal.
func extractTitle(s string) string {
	skip := map[string]bool{
		"summary:": true, "primary request and intent:": true,
		"primary request:": true, "the relationship": true,
	}
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		// Skip empty, short, or structural lines.
		if len(line) < 15 {
			continue
		}
		lower := strings.ToLower(line)
		if skip[lower] {
			continue
		}
		// Skip numbered section headers: "1. Foo:", "2. Bar and Baz:"
		if line != "" && line[0] >= '1' && line[0] <= '9' &&
			strings.HasPrefix(line[1:], ". ") && strings.HasSuffix(line, ":") {
			continue
		}
		// Skip markdown bold headers: "**Foo:**"
		if strings.HasPrefix(line, "**") && strings.HasSuffix(line, "**") {
			continue
		}
		// Found a real line.
		return truncate(line, 80)
	}
	return "Session memory"
}

// truncate returns s truncated to maxLen with "…" if needed.
func truncate(s string, maxLen int) string {
	// Use first non-empty line
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if len(line) <= maxLen {
			return line
		}
		return line[:maxLen-1] + "…"
	}
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-1] + "…"
}
