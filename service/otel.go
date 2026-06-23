package service

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
)

const tracerName = "scribe"

// Tracer returns the package-level tracer for creating spans.
func Tracer() trace.Tracer {
	return otel.Tracer(tracerName)
}

// InitTracer sets up the OpenTelemetry trace provider with OTLP HTTP export.
// Returns a shutdown function that flushes pending spans.
// If OTEL_EXPORTER_OTLP_ENDPOINT is not set, uses a no-op provider.
func InitTracer(ctx context.Context, version string) (shutdown func(context.Context) error, err error) {
	exporter, err := otlptracehttp.New(ctx)
	if err != nil {
		slog.WarnContext(ctx, "otel: OTLP exporter init failed, tracing disabled", slog.Any("error", err)) //nolint:sloglint // no constant
		return func(context.Context) error { return nil }, nil
	}

	res, _ := resource.Merge(
		resource.Default(),
		resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceName(tracerName),
			semconv.ServiceVersion(version),
		),
	)

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)

	return tp.Shutdown, nil
}
