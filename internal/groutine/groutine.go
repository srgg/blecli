package groutine

import (
	"bytes"
	"context"
	"runtime"
	"runtime/pprof"
	"strconv"
)

type ctxKey string

const goroutineNameKey ctxKey = "goroutine_name"

// Go starts a goroutine with a name, optional parent context
// Example usage:
//
//	gname.Go("worker-42", func(ctx context.Context) {
//	    // work
//	}, wg.Done)
//
// If parentCtx is nil, context.Background() is used.
func Go(parentCtx context.Context, name string, fn func(ctx context.Context)) {
	if parentCtx == nil {
		parentCtx = context.Background()
	}

	labels := pprof.Labels("goroutine_name", name)

	go pprof.Do(parentCtx, labels, func(ctx context.Context) {
		ctx = context.WithValue(ctx, goroutineNameKey, name)
		fn(ctx)
	})
}

// GetName retrieves the goroutine name from the context.
func GetName(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if v := ctx.Value(goroutineNameKey); v != nil {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// GetGID returns the numeric goroutine ID (hacky, for debugging).
func GetGID() uint64 {
	b := make([]byte, 64)
	b = b[:runtime.Stack(b, false)]
	b = bytes.TrimPrefix(b, []byte("goroutine "))
	i := bytes.IndexByte(b, ' ')
	if i < 0 {
		return 0
	}
	gid, _ := strconv.ParseUint(string(b[:i]), 10, 64)
	return gid
}
