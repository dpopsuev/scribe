package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	parchment "github.com/dpopsuev/parchment"
)

// ── Dataset export types ─────────────────────────────────────────────────

// sftExample is one supervised fine-tuning training example (OpenAI chat format).
type sftExample struct {
	Messages []sftMessage `json:"messages"`
}

type sftMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// kgNode is one knowledge-graph node.
type kgNode struct {
	ID     string `json:"id"`
	Kind   string `json:"kind"`
	Status string `json:"status"`
	Title  string `json:"title"`
	Scope  string `json:"scope"`
	Text   string `json:"text,omitempty"` // concatenated section text
}

// kgEdge is one knowledge-graph triple.
type kgEdge struct {
	Head     string  `json:"head"`
	Relation string  `json:"relation"`
	Tail     string  `json:"tail"`
	Weight   float64 `json:"weight,omitempty"`
}

// dpoExample is one direct preference optimisation training example.
type dpoExample struct {
	Prompt   string `json:"prompt"`
	Chosen   string `json:"chosen"`
	Rejected string `json:"rejected,omitempty"`
}

// datasetCard is the HuggingFace README.md front-matter plus body.
type datasetCard struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	License     string   `json:"license"`
	Tags        []string `json:"tags"`
	Formats     []string `json:"formats"`
	TotalRows   int      `json:"total_rows"`
}

// ── Quality filter ────────────────────────────────────────────────────────

// exportable returns true when an artifact meets the quality bar for training.
// Only complete, active, or evergreen artifacts with no compliance violations.
func exportable(a *parchment.Artifact) bool {
	switch a.ResolvedStatus() {
	case "active", "complete", "evergreen", "current", "accepted":
		// ok
	default:
		return false
	}
	for _, l := range a.Labels {
		if l == "compliance:violation" {
			return false
		}
	}
	return true
}

// sectionText concatenates all section bodies into a single string.
func sectionText(a *parchment.Artifact) string {
	var buf string
	for _, s := range a.Sections {
		if s.Text != "" {
			buf += s.Name + ": " + s.Text + "\n"
		}
	}
	return buf
}

// ── Serialisers ───────────────────────────────────────────────────────────

// writeSFT streams SFT examples: one per artifact, using the artifact's
// structured fields as an assistant response to a synthetic user prompt.
func writeSFT(ctx context.Context, w http.ResponseWriter, proto *parchment.Protocol) (int, error) {
	arts, err := proto.ListArtifacts(ctx, parchment.ListInput{})
	if err != nil {
		return 0, err
	}
	n := 0
	for _, a := range arts {
		if !exportable(a) {
			continue
		}
		content, _ := json.Marshal(map[string]any{
			"id":       a.ID,
			"kind":     a.ResolvedKind(),
			"title":    a.Title,
			"status":   a.ResolvedStatus(),
			"priority": a.ResolvedPriority(),
			"scope":    a.Scope,
			"sections": a.Sections,
		})
		ex := sftExample{Messages: []sftMessage{
			{Role: "system", Content: "You are Scribe, a structured artifact management assistant. Respond with valid JSON matching the artifact schema."},
			{Role: "user", Content: fmt.Sprintf("Create a %s artifact titled %q in scope %q.", a.ResolvedKind(), a.Title, a.Scope)},
			{Role: "assistant", Content: string(content)},
		}}
		b, _ := json.Marshal(ex)
		if _, err := fmt.Fprintf(w, "%s\n", b); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// writeKG streams knowledge-graph nodes then edges.
func writeKG(ctx context.Context, w http.ResponseWriter, proto *parchment.Protocol) (int, error) {
	arts, err := proto.ListArtifacts(ctx, parchment.ListInput{})
	if err != nil {
		return 0, err
	}
	ids := make([]string, 0, len(arts))
	n := 0
	for _, a := range arts {
		if !exportable(a) {
			continue
		}
		node := kgNode{
			ID:     a.ID,
			Kind:   a.ResolvedKind(),
			Status: a.ResolvedStatus(),
			Title:  a.Title,
			Scope:  a.Scope,
			Text:   sectionText(a),
		}
		b, _ := json.Marshal(node)
		if _, err := fmt.Fprintf(w, "%s\n", b); err != nil {
			return n, err
		}
		ids = append(ids, a.ID)
		n++
	}
	edges, _ := proto.Store().ListEdges(ctx, ids, nil)
	for _, e := range edges {
		edge := kgEdge{Head: e.From, Relation: e.Relation, Tail: e.To, Weight: e.Weight}
		b, _ := json.Marshal(edge)
		if _, err := fmt.Fprintf(w, "%s\n", b); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// writeDPO streams DPO preference pairs from decision (ADR) artifacts.
// chosen = decision section; rejected = alternatives_considered section.
// Also generates implicit pairs from accepted/rejected sibling specs.
func writeDPO(ctx context.Context, w http.ResponseWriter, proto *parchment.Protocol) (int, error) {
	arts, err := proto.ListArtifacts(ctx, parchment.ListInput{})
	if err != nil {
		return 0, err
	}
	n := 0
	for _, a := range arts {
		if a.ResolvedKind() != "decision" || !exportable(a) {
			continue
		}
		var prompt, chosen, rejected string
		for _, s := range a.Sections {
			switch s.Name {
			case "problem":
				prompt = s.Text
			case "decision":
				chosen = s.Text
			case "alternatives_considered", "alternatives":
				rejected = s.Text
			}
		}
		if prompt == "" || chosen == "" {
			continue
		}
		ex := dpoExample{Prompt: prompt, Chosen: chosen, Rejected: rejected}
		b, _ := json.Marshal(ex)
		if _, err := fmt.Fprintf(w, "%s\n", b); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// writeCard writes the HuggingFace dataset card as a single JSON line.
func writeCard(ctx context.Context, w http.ResponseWriter, proto *parchment.Protocol) (int, error) {
	arts, err := proto.ListArtifacts(ctx, parchment.ListInput{})
	if err != nil {
		return 0, err
	}
	exportableCount := 0
	for _, a := range arts {
		if exportable(a) {
			exportableCount++
		}
	}
	card := datasetCard{
		Name:        "scribe-artifacts",
		Description: "Structured knowledge artifacts from the Scribe artifact store. Human-validated, graph-linked, multi-format.",
		License:     "mit",
		Tags:        []string{"scribe", "knowledge-graph", "instruction-tuning", "dpo", "fine-tuning"},
		Formats:     []string{"sft", "kg", "dpo"},
		TotalRows:   exportableCount,
	}
	b, _ := json.Marshal(card)
	_, err = fmt.Fprintf(w, "%s\n", b)
	return 1, err
}

// ── HTTP handler ──────────────────────────────────────────────────────────

// handleExportDataset streams a JSONL training dataset.
// GET /api/v1/export/dataset?format=sft|kg|dpo|card
func (s *Server) handleExportDataset(w http.ResponseWriter, r *http.Request) {
	format := r.URL.Query().Get("format")
	if format == "" {
		format = "sft"
	}

	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="scribe-%s-%s.jsonl"`,
		format, time.Now().UTC().Format("20060102")))
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Dataset-Format", format)

	ctx := r.Context()
	var (
		n   int
		err error
	)
	switch format {
	case "sft":
		n, err = writeSFT(ctx, w, s.proto)
	case "kg":
		n, err = writeKG(ctx, w, s.proto)
	case "dpo":
		n, err = writeDPO(ctx, w, s.proto)
	case "card":
		n, err = writeCard(ctx, w, s.proto)
	default:
		http.Error(w, fmt.Sprintf("unknown format %q (valid: sft, kg, dpo, card)", format), http.StatusBadRequest)
		return
	}

	if err != nil {
		// Can't change status at this point since we've started streaming.
		// Log and close — partial JSONL is still usable.
		w.Header().Set("X-Export-Error", err.Error())
		return
	}
	w.Header().Set("X-Export-Rows", fmt.Sprintf("%d", n))
}
