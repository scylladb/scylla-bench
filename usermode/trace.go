package usermode

import (
	"context"
	"time"
)

type traceContextKey struct{}

// NopTrace is a general-purpose trace which tracing callbacks are nop.
var NopTrace = &Trace{
	ExecutedBatch: func(ExecutedBatchInfo) {},
}

// WithTrace gives new context with the given trace assigned.
func WithTrace(ctx context.Context, tr *Trace) context.Context {
	return context.WithValue(ctx, traceContextKey{}, tr)
}

// ContextTrace returns a trace read from the given context.
// If the context does not contain a trace, a NopTrace is
// returned instead.
func ContextTrace(ctx context.Context) *Trace {
	if tr, ok := ctx.Value(traceContextKey{}).(*Trace); ok {
		return tr
	}
	return NopTrace
}

// Trace holds callbacks which are called by the Driver after
// certain operations are completed altogether with their
// summary or results.
type Trace struct {
	ExecutedBatch func(ExecutedBatchInfo) // executed after batch insertion
}

// ExecutedBatchInfo describes batch insertion event.
type ExecutedBatchInfo struct {
	Size    int           // size of the batch
	Latency time.Duration // how long it took to insert
	Err     error         // err if insertion failed
}
