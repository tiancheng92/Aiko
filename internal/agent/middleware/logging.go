// internal/agent/middleware/logging.go
package middleware

import (
	"context"
	"log"
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
				log.Printf("[tool] %s error=%v elapsed=%s", name, err, elapsed)
			} else {
				log.Printf("[tool] %s elapsed=%s", name, elapsed)
			}
			return out, err
		}
	}
}
