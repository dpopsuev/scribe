package ingest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

// ErrUnexpectedStatus is returned when the server responds with a non-2xx status.
var ErrUnexpectedStatus = errors.New("ingest: unexpected status")

// Client streams NDJSON to Scribe's POST /api/v1/ingest endpoint.
// Locus (and other providers) import this package — no shell pipe, no curl.
type Client struct {
	BaseURL    string
	Source     string
	HTTPClient *http.Client // nil uses http.DefaultClient
}

// Stream encodes nodes and edges as NDJSON and POSTs to /api/v1/ingest.
// Uses io.Pipe so the HTTP body is written incrementally — memory cost is
// bounded by the pipe buffer regardless of how many records are sent.
func (c *Client) Stream(ctx context.Context, nodes []NodeRecord, edges []EdgeRecord) (*Result, error) {
	pr, pw := io.Pipe()

	go func() {
		enc := json.NewEncoder(pw)
		enc.SetEscapeHTML(false)
		var encErr error
		for i := range nodes {
			nodes[i].Type = "node"
			if encErr = enc.Encode(nodes[i]); encErr != nil {
				break
			}
		}
		if encErr == nil {
			for i := range edges {
				edges[i].Type = "edge"
				if encErr = enc.Encode(edges[i]); encErr != nil {
					break
				}
			}
		}
		if encErr == nil {
			encErr = enc.Encode(MetaRecord{
				Type:       "meta",
				Source:     c.Source,
				TotalNodes: len(nodes),
				TotalEdges: len(edges),
			})
		}
		pw.CloseWithError(encErr) //nolint:errcheck // pipe close; error surfaced via pr
	}()

	url := fmt.Sprintf("%s/api/v1/ingest?source=%s", c.BaseURL, c.Source)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, pr)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-ndjson")

	hc := c.HTTPClient
	if hc == nil {
		hc = http.DefaultClient
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("POST ingest: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // response body drained into body below; close error is irrelevant

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusMultiStatus {
		return nil, fmt.Errorf("%w: %d %s", ErrUnexpectedStatus, resp.StatusCode, bytes.TrimSpace(body))
	}

	var result Result
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("decode result: %w", err)
	}
	return &result, nil
}
