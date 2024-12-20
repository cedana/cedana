package types

// A profiler is just an adapter, that records profiling data for the next handler.
// Profilers can be chained in between adapters to record profiling data for each one.

import (
	"context"
	"reflect"

	"github.com/cedana/cedana/pkg/profiling"
)

// Generic profiler for recording timing information. Populates the profiling data for the next handler.
// Thus, the handler can simply populate more components if it wants to. The total time will be recorded
// here itself.
func Timing[REQ, RESP any](next Handler[REQ, RESP]) Handler[REQ, RESP] {
	var timedHandler Handler[REQ, RESP]
	nextPc := reflect.ValueOf(next).Pointer()

	timedHandler = func(ctx context.Context, server ServerOpts, resp *RESP, req *REQ) (chan int, error) {
		// We skip profiling if the next handler is itself
		if nextPc != reflect.ValueOf(timedHandler).Pointer() {
			var end func()
			ctx, end = profiling.StartTiming(ctx, next)
			defer end()
		}

		return next(ctx, server, resp, req)
	}

	return timedHandler
}
