// internal/agent/middleware/logging.go
package middleware

import (
	"context"
	"log/slog"
	"time"
)

// Logging returns a Middleware that logs each tool invocation with its duration.
func Logging() Middleware {
	return func(name string, next Handler) Handler {
		return func(ctx context.Context, input string) (string, error) {
			start := time.Now()
			out, err := next(ctx, input)
			elapsed := time.Since(start)
			if err != nil {
				slog.Error("tool invocation failed", "tool", name, "err", err, "elapsed", elapsed)
			} else {
				slog.Debug("tool invoked", "tool", name, "elapsed", elapsed)
			}
			return out, err
		}
	}
}
