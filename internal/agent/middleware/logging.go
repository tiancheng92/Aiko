// internal/agent/middleware/logging.go
package middleware

import (
	"context"
	"time"

	"github.com/rs/zerolog/log"
)

// Logging returns a Middleware that logs each tool invocation with its duration,
// input arguments, and output result.
func Logging() Middleware {
	return func(name string, next Handler) Handler {
		return func(ctx context.Context, input string) (string, error) {
			start := time.Now()
			log.Debug().Str("tool", name).Str("args", input).Msg("tool call")
			out, err := next(ctx, input)
			elapsed := time.Since(start)
			if err != nil {
				log.Error().Str("tool", name).Str("args", input).Err(err).Dur("elapsed", elapsed).Msg("tool invocation failed")
			} else {
				log.Debug().Str("tool", name).Str("result", out).Dur("elapsed", elapsed).Msg("tool result")
			}
			return out, err
		}
	}
}
