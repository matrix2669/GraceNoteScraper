package dispatcharr

import (
	"context"
	"fmt"
)

type progressContextKey struct{}

// WithMatchProgress observes completed channels without changing matching inputs.
func WithMatchProgress(ctx context.Context, notify func(int, int)) context.Context {
	return context.WithValue(ctx, progressContextKey{}, notify)
}

func matcherProgressCallback(ctx context.Context) func(int, int) {
	notify, _ := ctx.Value(progressContextKey{}).(func(int, int))
	return notify
}

// Discard diagnostics and oversized lines; never expose subprocess stderr.
type matcherProgressWriter struct {
	line     []byte
	overflow bool
	notify   func(int, int)
}

func (w *matcherProgressWriter) Write(p []byte) (int, error) {
	for _, b := range p {
		if b == '\n' {
			var done, total int
			if !w.overflow && w.notify != nil {
				if n, err := fmt.Sscanf(string(w.line), "GNS_PROGRESS %d %d", &done, &total); err == nil && n == 2 && done >= 0 && total > 0 && done <= total {
					w.notify(done, total)
				}
			}
			w.line = w.line[:0]
			w.overflow = false
		} else if len(w.line) < 128 && !w.overflow {
			w.line = append(w.line, b)
		} else {
			w.overflow = true
		}
	}
	return len(p), nil
}
