// internal/agent/middleware/retry.go
package middleware

import (
	"context"
	"time"
)

// maxRetryDelay caps the exponential back-off so a misconfigured caller
// (very large baseDelay or many attempts) can't overflow or block for hours.
const maxRetryDelay = 60 * time.Second

// Retry returns a Middleware that retries up to maxAttempts times with
// exponential back-off starting at baseDelay. Only hard errors trigger retries;
// soft string results pass through immediately.
func Retry(maxAttempts int, baseDelay time.Duration) Middleware {
	return func(name string, next Handler) Handler {
		return func(ctx context.Context, input string) (string, error) {
			var (
				out string
				err error
			)
			delay := baseDelay
			for attempt := 1; attempt <= maxAttempts; attempt++ {
				out, err = next(ctx, input)
				if err == nil {
					return out, nil
				}
				if attempt == maxAttempts {
					break
				}
				select {
				case <-ctx.Done():
					return "", ctx.Err()
				case <-time.After(delay):
				}
				delay *= 2
				if delay > maxRetryDelay {
					delay = maxRetryDelay
				}
			}
			return out, err
		}
	}
}
