package ingest

import (
	"fmt"
	"time"
)

// AgentSession identifies one conversation between a user and an LLM.
type AgentSession struct {
	ID     string // stable identifier, e.g. a UUID
	Source string // "alef", "djinn", "faux-agent", …
	CWD    string // working directory hash or path
	Model  string // LLM model name
}

// AgentToolCall records one tool invocation within a turn.
type AgentToolCall struct {
	Name   string // e.g. "file_read", "shell_exec", "scribe_create_artifact"
	Input  string // serialized arguments
	Output string // serialized result
}

// AgentTurn is one complete user→(tool*)→assistant exchange.
type AgentTurn struct {
	ID            string
	SessionID     string
	Index         int // 0-based turn counter within the session
	UserText      string
	AssistantText string
	ToolCalls     []AgentToolCall
	// Extra labels the caller wants on the node beyond the standard set.
	Labels []string
	// Extra key-value metadata (model, token counts, timestamps, …).
	Extra map[string]any
}

// TurnToNodeRecord maps an AgentTurn to an IngestNodeRecord.
// The resulting node has stable ID "source:session_id:turn_index" so that
// re-ingesting the same turn is an idempotent upsert.
func TurnToNodeRecord(turn AgentTurn) NodeRecord {
	toolNames := make([]string, len(turn.ToolCalls))
	for i, tc := range turn.ToolCalls {
		toolNames[i] = tc.Name
	}

	extra := map[string]any{
		"turn_index": turn.Index,
		"session_id": turn.SessionID,
		"scanned_at": time.Now().UTC().Format(time.RFC3339),
		"tools_used": toolNames,
	}
	for k, v := range turn.Extra {
		extra[k] = v
	}

	labels := make([]string, 0, 3+len(turn.Labels))
	labels = append(labels,
		"source:"+extractSource(turn.ID),
		"session:"+turn.SessionID,
		"kind:turn",
	)
	labels = append(labels, turn.Labels...)

	sections := make([]Section, 0, 2+len(turn.ToolCalls))
	sections = append(sections,
		Section{Name: "user", Text: turn.UserText},
		Section{Name: "assistant", Text: turn.AssistantText},
	)
	for i, tc := range turn.ToolCalls {
		sections = append(sections, Section{
			Name: fmt.Sprintf("tool-%d-%s", i, tc.Name),
			Text: fmt.Sprintf("input: %s\noutput: %s", tc.Input, tc.Output),
		})
	}

	return NodeRecord{
		Type:     "node",
		ID:       turn.ID,
		Kind:     "note",
		Title:    fmt.Sprintf("Turn %d: %s", turn.Index, truncate(turn.UserText, 60)),
		Status:   "active",
		Labels:   labels,
		Extra:    extra,
		Sections: sections,
	}
}

// TurnToEdges produces edges from a turn node to referenced artifacts.
// Currently handles:
//   - traces_to edges for file_read/file_write tool calls (→ code symbols)
//   - produces edges for create_artifact tool calls (→ new artifact)
func TurnToEdges(turn AgentTurn) []EdgeRecord {
	var edges []EdgeRecord
	for _, tc := range turn.ToolCalls {
		switch tc.Name {
		case "file_read", "file_write", "fs.read", "fs.write":
			if tc.Input != "" {
				edges = append(edges, EdgeRecord{
					Type:     "edge",
					From:     turn.ID,
					To:       "locus:symbol:" + tc.Input,
					Relation: "traces_to",
				})
			}
		case "scribe_create_artifact", "create_artifact":
			if tc.Output != "" {
				edges = append(edges, EdgeRecord{
					Type:     "edge",
					From:     turn.ID,
					To:       tc.Output,
					Relation: "produces",
				})
			}
		}
	}
	return edges
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// extractSource reads the source prefix from a turn ID formatted as
// "source:session:index". Falls back to "agent" if the format doesn't match.
func extractSource(id string) string {
	for i, c := range id {
		if c == ':' {
			return id[:i]
		}
	}
	return "agent"
}
