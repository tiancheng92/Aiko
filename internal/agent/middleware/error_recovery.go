// internal/agent/middleware/error_recovery.go
package middleware

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino/compose"
)

// ErrorRecovery returns a Middleware that catches panics and converts hard errors
// into user-friendly strings so the LLM conversation is never interrupted.
// Interrupt signals (from tool.Interrupt) are passed through unchanged so the
// agent runner can handle the checkpoint/resume flow.
func ErrorRecovery() Middleware {
	return func(name string, next Handler) Handler {
		return func(ctx context.Context, input string) (out string, err error) {
			defer func() {
				if r := recover(); r != nil {
					out = fmt.Sprintf("工具 %q 遇到意外错误，已跳过: %v", name, r)
					err = nil
				}
			}()
			out, err = next(ctx, input)
			if err != nil {
				// Pass interrupt signals through so the runner can checkpoint/resume.
				if _, ok := compose.IsInterruptRerunError(err); ok {
					return
				}
				// Convert all other hard errors to soft strings so eino doesn't abort.
				out = fmt.Sprintf("工具 %q 执行出错: %v", name, err)
				err = nil
			}
			return
		}
	}
}
