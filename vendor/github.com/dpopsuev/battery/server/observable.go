package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"
)

// Log attribute keys for observable tool calls.
const (
	LogKeyTool    = "tool"
	LogKeyElapsed = "elapsed"
	LogKeyError   = "error"
)

// Observable wraps a Handler with timing and error logging.
func Observable(name string, h Handler) Handler {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		start := time.Now()
		result, err := h(ctx, input)
		elapsed := time.Since(start)

		if err != nil {
			slog.WarnContext(ctx, "battery: tool failed",
				slog.String(LogKeyTool, name),
				slog.Duration(LogKeyElapsed, elapsed),
				slog.String(LogKeyError, err.Error()),
			)
		} else {
			slog.DebugContext(ctx, "battery: tool completed",
				slog.String(LogKeyTool, name),
				slog.Duration(LogKeyElapsed, elapsed),
			)
		}
		return result, err
	}
}

// TextResult wraps a string as a result.
func TextResult(s string) string { return s }

// JSONResult marshals data as a JSON string result.
func JSONResult(data any) (string, error) {
	b, err := json.Marshal(data)
	if err != nil {
		return "", fmt.Errorf("battery: json result: %w", err)
	}
	return string(b), nil
}
