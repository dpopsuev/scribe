// Package client provides a thin HTTP client for posting records to Scribe's
// ingest endpoint. No domain knowledge — accepts canonical translate.Record
// and posts NDJSON.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/dpopsuev/battery/translate"
)

// ErrIngestFailed is returned when the Scribe ingest endpoint returns a 4xx/5xx status.
var ErrIngestFailed = fmt.Errorf("scribe ingest failed")

const ndjsonType = "type"

// Post translates Records + Edges to NDJSON and POSTs to the Scribe ingest URL.
func Post(ctx context.Context, records []translate.Record, edges []translate.Edge, source, ingestURL string) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)

	for _, r := range records {
		_ = enc.Encode(map[string]any{
			ndjsonType: "node", "id": r.ID, "kind": r.Kind,
			"title": r.Title, "labels": r.Labels,
			"extra": r.Extra, "sections": r.Sections,
		})
	}
	for _, e := range edges {
		_ = enc.Encode(map[string]any{
			ndjsonType: "edge", "from": e.From, "to": e.To, "relation": e.Relation,
		})
	}
	_ = enc.Encode(map[string]any{
		ndjsonType:    "meta",
		"source":      source,
		"scanned_at":  time.Now().UTC().Format(time.RFC3339),
		"total_nodes": len(records),
		"total_edges": len(edges),
	})

	url := fmt.Sprintf("%s?source=%s", ingestURL, source)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, &buf)
	if err != nil {
		return fmt.Errorf("build ingest request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-ndjson")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return FormatUnavailable(ingestURL, err)
	}
	defer func() { _ = resp.Body.Close() }()

	const httpClientError = 400
	if resp.StatusCode >= httpClientError {
		return fmt.Errorf("%w: HTTP %d", ErrIngestFailed, resp.StatusCode)
	}
	return nil
}

// FormatUnavailable turns dial/transport failures into an actionable recovery hint.
func FormatUnavailable(addr string, err error) error {
	if addr == "" {
		addr = "localhost:8080"
	}
	return fmt.Errorf("scribe MCP unavailable at %s — start with: systemctl --user start container-scribe.service (or: scribe serve --transport http --addr :8080); underlying: %w", addr, err)
}
