// internal/agent/middleware/chain.go
package middleware

import (
	"context"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// Handler is a function that executes a single tool invocation.
type Handler func(ctx context.Context, input string) (string, error)

// Middleware wraps a Handler to add cross-cutting behavior.
type Middleware func(name string, next Handler) Handler

// Chain applies middlewares right-to-left so the first middleware is outermost.
func Chain(middlewares ...Middleware) Middleware {
	return func(name string, next Handler) Handler {
		for i := len(middlewares) - 1; i >= 0; i-- {
			next = middlewares[i](name, next)
		}
		return next
	}
}

// wrappedTool applies a middleware chain around an eino BaseTool.
type wrappedTool struct {
	inner   tool.BaseTool
	handler Handler
}

// Wrap returns a new BaseTool whose InvokableRun is wrapped by the given chain.
func Wrap(t tool.BaseTool, chain Middleware) tool.BaseTool {
	info, _ := t.Info(context.Background())
	name := ""
	if info != nil {
		name = info.Name
	}
	invokable, ok := t.(interface {
		InvokableRun(context.Context, string, ...tool.Option) (string, error)
	})
	if !ok {
		return t // can't wrap, return as-is
	}
	base := Handler(func(ctx context.Context, input string) (string, error) {
		return invokable.InvokableRun(ctx, input)
	})
	return &wrappedTool{inner: t, handler: chain(name, base)}
}

// WrapAll applies Wrap to every tool in the slice.
func WrapAll(tools []tool.BaseTool, chain Middleware) []tool.BaseTool {
	result := make([]tool.BaseTool, len(tools))
	for i, t := range tools {
		result[i] = Wrap(t, chain)
	}
	return result
}

func (w *wrappedTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return w.inner.Info(ctx)
}

func (w *wrappedTool) InvokableRun(ctx context.Context, input string, _ ...tool.Option) (string, error) {
	return w.handler(ctx, input)
}
