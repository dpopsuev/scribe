package embed

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	parchment "github.com/dpopsuev/parchment"
)

// Extraction holds auto-extracted metadata from artifact content.
type Extraction struct {
	People      []string `json:"people"`
	Topics      []string `json:"topics"`
	ActionItems []string `json:"action_items"`
	Dates       []string `json:"dates_mentioned"`
	Type        string   `json:"type"`
}

// ExtractFunc calls an LLM to extract structured metadata from text.
type ExtractFunc func(ctx context.Context, text string) (*Extraction, error)

// OllamaExtractFunc returns an ExtractFunc that calls Ollama's generate endpoint.
func OllamaExtractFunc(ollamaURL, model string) ExtractFunc {
	client := &http.Client{Timeout: 30 * time.Second}
	base := strings.TrimRight(ollamaURL, "/")
	return func(ctx context.Context, text string) (*Extraction, error) {
		prompt := fmt.Sprintf(`Extract metadata from this text. Return ONLY valid JSON with these fields:
- "people": array of people mentioned (empty if none)
- "topics": array of 1-3 short topic tags (always at least one)
- "action_items": array of implied to-dos (empty if none)
- "dates_mentioned": array of dates in YYYY-MM-DD format (empty if none)
- "type": one of "observation", "task", "idea", "reference", "note"

Text: %s`, text)

		body, _ := json.Marshal(map[string]any{
			"model":      model,
			"prompt":     prompt,
			"stream":     false,
			"keep_alive": -1,
			"format":     "json",
		})
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/api/generate", bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close() //nolint:errcheck // deferred close on read-only response body
		if resp.StatusCode != http.StatusOK {
			raw, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
			return nil, fmt.Errorf("ollama HTTP %d: %s", resp.StatusCode, raw) //nolint:err113 // status+body runtime values
		}
		var result struct {
			Response string `json:"response"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return nil, fmt.Errorf("decode: %w", err)
		}
		var extraction Extraction
		if err := json.Unmarshal([]byte(result.Response), &extraction); err != nil {
			return nil, fmt.Errorf("parse extraction: %w", err)
		}
		return &extraction, nil
	}
}

// ExtractAndStamp runs extraction on an artifact and merges results into Extra.
func ExtractAndStamp(ctx context.Context, store parchment.Store, extractFn ExtractFunc, art *parchment.Artifact) error {
	text := art.Title
	if goal := art.Goal(); goal != "" {
		text += "\n" + goal
	}
	for _, sec := range art.Sections {
		text += "\n" + sec.Text
	}
	if len(text) > 4000 {
		text = text[:4000]
	}

	extraction, err := extractFn(ctx, text)
	if err != nil {
		return err
	}

	extra := art.Extra
	if extra == nil {
		extra = make(map[string]any)
	}
	if len(extraction.People) > 0 {
		extra["people"] = extraction.People
	}
	if len(extraction.Topics) > 0 {
		extra["topics"] = extraction.Topics
	}
	if len(extraction.ActionItems) > 0 {
		extra["action_items"] = extraction.ActionItems
	}
	if len(extraction.Dates) > 0 {
		extra["dates_mentioned"] = extraction.Dates
	}
	if extraction.Type != "" {
		extra["extracted_type"] = extraction.Type
	}
	extra["extracted"] = true

	return store.PatchArtifact(ctx, art.ID, parchment.ArtifactPatch{
		SetExtra: extra,
	})
}
