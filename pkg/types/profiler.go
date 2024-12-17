package types

// A profiler is just an adapter

import (
	"context"
	"reflect"
	"time"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/utils"
	"github.com/rs/zerolog/log"
)

// Generic timing adapter that adds timing data for the next handler.
func Timed[REQ, RESP any](next Handler[REQ, RESP]) Handler[REQ, RESP] {
	var timedHandler Handler[REQ, RESP]

	record := func(start time.Time, profiling *daemon.ProfilingData) {
		elapsed := time.Since(start)

		elapsedMs := float64(elapsed.Nanoseconds()) / 1e6

		thisPc := reflect.ValueOf(timedHandler).Pointer()
		nextPc := reflect.ValueOf(next).Pointer()
		if thisPc == nextPc {
			// skip since 2 timing adapters are chained
			return
		}
		profiling.Duration = elapsedMs
		if profiling.Name == "" { // only override if not pre-set
			profiling.Name = utils.FunctionName(nextPc)
		}
		log.Trace().Str("profiling", profiling.Name).Msgf("took %s", elapsed)
	}

	timedHandler = func(ctx context.Context, server ServerOpts, resp *RESP, req *REQ) (chan int, error) {
		if server.Profiling != nil {
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
