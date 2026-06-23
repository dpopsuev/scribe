package service

import (
	"context"
	"encoding/json"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// Op is a single named operation exposed on both the CLI and MCP surfaces.
// Run receives the raw JSON input (already parsed from flags or MCP request),
// executes the operation against svc, and returns human-readable text output.
// The caller is responsible for presenting the text (MCP wraps it in a tool
// result; CLI prints it to stdout).
type Op struct {
	Name string
	Run  func(ctx context.Context, svc *Service, in json.RawMessage) (string, error)
}

// RunTraced wraps Op.Run with an OpenTelemetry span.
func (o *Op) RunTraced(ctx context.Context, svc *Service, in json.RawMessage) (string, error) {
	ctx, span := Tracer().Start(ctx, "op."+o.Name, withOpAttributes(o.Name, in))
	defer span.End()
	out, err := o.Run(ctx, svc, in)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	return out, err
}

func withOpAttributes(name string, in json.RawMessage) trace.SpanStartOption {
	return trace.WithAttributes(
		attribute.String("scribe.op", name),
		attribute.Int("scribe.input_bytes", len(in)),
	)
}

// Registry is the global operation table. Both the MCP handlers and the CLI
// command constructors iterate this slice. Entries are added here as
// operations are migrated from their respective switch cases.
var Registry []Op

// Find returns the Op with the given name, or nil if not found.
func Find(name string) *Op {
	for i := range Registry {
		if Registry[i].Name == name {
			return &Registry[i]
		}
	}
	return nil
}
