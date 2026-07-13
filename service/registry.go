package service

import (
	"context"
	"encoding/json"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// Op is a single named operation exposed on both the CLI and MCP surfaces.
// Run receives the raw JSON input and returns human-readable text.
// Structured, when set, is preferred by MCP and returns typed Data for
// structuredContent while still populating Text for display.
type Op struct {
	Name       string
	Run        func(ctx context.Context, svc *Service, in json.RawMessage) (string, error)
	Structured func(ctx context.Context, svc *Service, in json.RawMessage) (Result, error)
}

// Execute runs Structured when present, otherwise Run wrapped as Result.
func (o *Op) Execute(ctx context.Context, svc *Service, in json.RawMessage) (Result, error) {
	if o.Structured != nil {
		return o.Structured(ctx, svc, in)
	}
	text, err := o.Run(ctx, svc, in)
	return Result{Text: text}, err
}

// RunTraced wraps Execute with an OpenTelemetry span.
func (o *Op) RunTraced(ctx context.Context, svc *Service, in json.RawMessage) (Result, error) {
	ctx, span := Tracer().Start(ctx, "op."+o.Name, withOpAttributes(o.Name, in))
	defer span.End()
	out, err := o.Execute(ctx, svc, in)
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
