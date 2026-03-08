package mcpclient

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// LocusClient is a lightweight MCP client for calling Locus tools.
type LocusClient struct {
	url     string
	session *sdkmcp.ClientSession
}

// DefaultLocusURL returns the configured or default Locus endpoint.
func DefaultLocusURL() string {
	if v := os.Getenv("LOCUS_URL"); v != "" {
		return v
	}
	return "http://localhost:8081/"
}

// New creates a LocusClient that connects to the given HTTP endpoint.
func New(locusURL string) *LocusClient {
	return &LocusClient{url: locusURL}
}

func (c *LocusClient) connect(ctx context.Context) error {
	if c.session != nil {
		return nil
	}
	transport := &sdkmcp.StreamableClientTransport{Endpoint: c.url}
	client := sdkmcp.NewClient(&sdkmcp.Implementation{
		Name:    "scribe-locus-client",
		Version: "0.1.0",
	}, nil)
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		return fmt.Errorf("connect to locus at %s: %w", c.url, err)
	}
	c.session = session
	return nil
}

// Close shuts down the MCP client connection.
func (c *LocusClient) Close() error {
	if c.session != nil {
		return c.session.Close()
	}
	return nil
}

// ScanProject calls locus scan_project and returns the raw JSON result.
func (c *LocusClient) ScanProject(ctx context.Context, path string) (json.RawMessage, error) {
	return c.callTool(ctx, "scan_project", map[string]any{"path": path})
}

// GetCycles calls locus get_cycles.
func (c *LocusClient) GetCycles(ctx context.Context, path string) (json.RawMessage, error) {
	return c.callTool(ctx, "get_cycles", map[string]any{"path": path})
}

// GetAPISurface calls locus get_api_surface.
func (c *LocusClient) GetAPISurface(ctx context.Context, path string) (json.RawMessage, error) {
	return c.callTool(ctx, "get_api_surface", map[string]any{"path": path})
}

func (c *LocusClient) callTool(ctx context.Context, name string, args map[string]any) (json.RawMessage, error) {
	if err := c.connect(ctx); err != nil {
		return nil, err
	}
	result, err := c.session.CallTool(ctx, &sdkmcp.CallToolParams{
		Name:      name,
		Arguments: args,
	})
	if err != nil {
		return nil, fmt.Errorf("call %s: %w", name, err)
	}
	if result.IsError {
		return nil, fmt.Errorf("tool %s returned error", name)
	}
	for _, content := range result.Content {
		if tc, ok := content.(*sdkmcp.TextContent); ok {
			return json.RawMessage(tc.Text), nil
		}
	}
	return nil, fmt.Errorf("tool %s returned no text content", name)
}
