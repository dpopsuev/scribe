package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"
)

// Observable wraps a Handler with timing and error logging.
func Observable(name string, h Handler) Handler {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		start := time.Now()
		result, err := h(ctx, input)
		elapsed := time.Since(start)

		if err != nil {
			slog.Warn("battery: tool failed",
				slog.String("tool", name),
				slog.Duration("elapsed", elapsed),
				slog.String("err", err.Error()),
			)
		} else {
			slog.Debug("battery: tool completed",
				slog.String("tool", name),
				slog.Duration("elapsed", elapsed),
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
