package service

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	parchment "github.com/dpopsuev/parchment"
)

// IngestSessionResult holds the outcome of an ingest_session operation.
type IngestSessionResult struct {
	Paths   []string
	Created int
	Skipped int
	Errors  []string
	Scope   string
}

// IngestSession imports Pi and Claude Code JSONL session files into the knowledge vault.
func (s *Service) IngestSession(ctx context.Context, path, scope string) (*IngestSessionResult, error) {
	if path == "" {
		return nil, fmt.Errorf("path is required") //nolint:err113 // agent-facing, inline message is the contract
	}
	if scope == "" && len(s.HomeScopes) > 0 {
		scope = s.HomeScopes[0]
	}

	fi, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("cannot access path %q: %w", path, err)
	}

	var paths []string
	if fi.IsDir() {
		entries, err := os.ReadDir(path)
		if err != nil {
			return nil, fmt.Errorf("cannot read directory: %w", err)
		}
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".jsonl") {
				paths = append(paths, filepath.Join(path, e.Name()))
			}
		}
	} else {
		paths = []string{path}
	}

	result := &IngestSessionResult{Paths: paths, Scope: scope}
	for _, p := range paths {
		n, sk, err := s.ingestSessionFile(ctx, p, scope)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", filepath.Base(p), err))
			continue
		}
		result.Created += n
		result.Skipped += sk
	}
	return result, nil
}

func (s *Service) ingestSessionFile(ctx context.Context, path, scope string) (created, skipped int, err error) {
	existing, _ := s.Proto.ListArtifacts(ctx, parchment.ListInput{Labels: []string{parchment.LabelPrefixKind + parchment.KindSource}, Scope: scope})
	for _, src := range existing {
		for _, sec := range src.Sections {
			if sec.Name == "provenance" && strings.Contains(sec.Text, path) {
				return 0, 1, nil
			}
		}
	}

	sess, err := parseSessionJSONL(path)
	if err != nil {
		return 0, 0, err
	}

	title := fmt.Sprintf("Session: %s", filepath.Base(path))
	if sess.cwd != "" {
		title = fmt.Sprintf("Session: %s (%s)", filepath.Base(path), filepath.Base(sess.cwd))
	}
	source, err := s.Proto.CreateArtifact(ctx, parchment.CreateInput{
		Labels: []string{parchment.LabelPrefixKind + parchment.KindSource},
		Title:  title, Scope: scope,
		Sections: []parchment.Section{
			{Name: "provenance", Text: path},
			{Name: "summary", Text: sess.summary()},
		},
	})
	if err != nil {
		return 0, 0, fmt.Errorf("create source: %w", err)
	}
	created++

	for _, c := range sess.compactions {
		if c == "" {
			continue
		}
		art, err := s.Proto.CreateArtifact(ctx, parchment.CreateInput{
			Labels: []string{parchment.LabelPrefixKind + parchment.KindContext},
			Title:  ExtractTitle(c), Scope: scope,
			Sections: []parchment.Section{{Name: "summary", Text: c}},
		})
		if err != nil {
			continue
		}
		_, _ = s.Proto.LinkArtifacts(ctx, art.ID, parchment.RelCites, []string{source.ID}, 0)
		created++
	}
	return created, 0, nil
}

type sessionData struct {
	format      string
	cwd         string
	compactions []string
	toolCalls   int
}

func (sd *sessionData) summary() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Format: %s\n", sd.format)
	if sd.cwd != "" {
		fmt.Fprintf(&b, "CWD: %s\n", sd.cwd)
	}
	fmt.Fprintf(&b, "Compaction summaries: %d\n", len(sd.compactions))
	fmt.Fprintf(&b, "Scribe tool calls observed: %d\n", sd.toolCalls)
	return b.String()
}

func parseSessionJSONL(path string) (*sessionData, error) {
	f, err := os.Open(path) //nolint:gosec // operator-controlled path
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
		case "session":
			sess.format = "pi"
			sess.cwd = jsonString(entry["cwd"])
		case "compaction", "branch_summary":
			if s := jsonString(entry["summary"]); s != "" {
				sess.compactions = append(sess.compactions, s)
			}
		case "message":
			sess.toolCalls += countScribeToolCallsPi(entry["message"])
		case "assistant":
			if sess.format == "" {
				sess.format = "claude"
			}
			sess.toolCalls += countScribeToolCallsClaude(entry["message"])
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

// ExtractTitle returns a meaningful title from a compaction summary.
func ExtractTitle(s string) string {
	skip := map[string]bool{
		"summary:": true, "primary request and intent:": true,
		"primary request:": true, "the relationship": true,
	}
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if len(line) < 15 {
			continue
		}
		lower := strings.ToLower(line)
		if skip[lower] {
			continue
		}
		if line[0] >= '1' && line[0] <= '9' &&
			strings.HasPrefix(line[1:], ". ") && strings.HasSuffix(line, ":") {
			continue
		}
		if strings.HasPrefix(line, "**") && strings.HasSuffix(line, "**") {
			continue
		}
		return Truncate(line, 80)
	}
	return "Session memory"
}

// Truncate returns s truncated to maxLen with "…" if needed.
func Truncate(s string, maxLen int) string {
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
