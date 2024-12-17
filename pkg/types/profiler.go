package types

// A profiler is just an adapter

import (
	"context"
	"reflect"
	"time"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/utils"
)

// Generic timing adapter that adds timing data for the next handler.
func Timed[REQ, RESP any](next Handler[REQ, RESP]) Handler[REQ, RESP] {
	var timedHandler Handler[REQ, RESP]
	nextPc := reflect.ValueOf(next).Pointer()

	record := func(start time.Time, profiling *daemon.ProfilingData) {
		elapsed := time.Since(start)

		elapsedMs := float64(elapsed.Nanoseconds()) / 1e6

		profiling.Duration = elapsedMs
		profiling.Name = utils.FunctionName(nextPc)
	}

	timedHandler = func(ctx context.Context, server ServerOpts, resp *RESP, req *REQ) (chan int, error) {
		// We also skip profiling if the next handler is the profiler itself
		if server.Profiling != nil && nextPc != reflect.ValueOf(timedHandler).Pointer() {
			defer record(time.Now(), server.Profiling)

			newProfilingData := &daemon.ProfilingData{}
			if server.Profiling.Components == nil {
				server.Profiling.Components = []*daemon.ProfilingData{}
			}
			server.Profiling.Components = append(server.Profiling.Components, newProfilingData)
			server.Profiling = newProfilingData
		}

		return next(ctx, server, resp, req)
	}

	return timedHandler
}
