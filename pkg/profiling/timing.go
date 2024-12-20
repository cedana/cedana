package profiling

import (
	"context"
	"reflect"
	"runtime"
	"time"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/internal/metrics"
	"github.com/cedana/cedana/pkg/keys"
	"github.com/cedana/cedana/pkg/utils"
	"github.com/rs/zerolog/log"
	"go.opentelemetry.io/otel"
)

// StartTiming starts a timer and returns a function that should be called to end the timer.
// If no f is provided, uses the caller to get the function name.
func StartTiming(ctx context.Context, f ...any) (childCtx context.Context, end func()) {
	var data *daemon.ProfilingData
	data, ok := ctx.Value(keys.PROFILING_CONTEXT_KEY).(*daemon.ProfilingData)
	if !ok {
		data = &daemon.ProfilingData{}
	}

	if data.Name == "" {
		var pc uintptr
		if len(f) == 0 {
			pc, _, _, _ = runtime.Caller(1)
		} else {
			pc = reflect.ValueOf(f[0]).Pointer()
		}

		data.Name = utils.FunctionName(pc)
	}

	start := time.Now()
	childCtx, span := otel.Tracer(metrics.API_TRACER).Start(ctx, data.Name)
	defer span.End()

	end = func() {
		duration := time.Since(start)
		span.End()
		data.Duration = duration.Nanoseconds()

		log.Trace().Str("in", data.Name).Msgf("spent %s", duration)
	}

	component := &daemon.ProfilingData{}
	data.Components = append(data.Components, component)
	childCtx = context.WithValue(childCtx, keys.PROFILING_CONTEXT_KEY, component)

	return
}

// StartTimingComponent starts a timer and returns a function that should be called to end the timer.
// Unlike StartTiming, this adds the data as a new component of the profiling data.
func StartTimingComponent(ctx context.Context, f ...any) (childCtx context.Context, end func()) {
	var data *daemon.ProfilingData
	data, ok := ctx.Value(keys.PROFILING_CONTEXT_KEY).(*daemon.ProfilingData)
	if !ok {
		data = &daemon.ProfilingData{}
	}

	var pc uintptr
	if len(f) == 0 {
		pc, _, _, _ = runtime.Caller(1)
	} else {
		pc = reflect.ValueOf(f[0]).Pointer()
	}
	name := utils.FunctionName(pc)

	component := &daemon.ProfilingData{Name: name}

	data.Components = append(profiling.Components, component)

	log.Trace().Str("in", name).Msgf("spent %s", duration)
	return component
}

// StartTimingCategory starts a timer and returns a function that should be called to end the timer.
// Instead of directly inserting a component like StartTimingComponent, this adds the data as a nested component,
// with the name matching the category provided. Also returns the component that was added to the category.
func StartTimingCategory(start time.Time, profiling *daemon.ProfilingData, category string, f ...any) *daemon.ProfilingData {
	var categoryComponent *daemon.ProfilingData
	for _, component := range profiling.Components {
		if component.Name == category {
			categoryComponent = component
			break
		}
	}
	if categoryComponent == nil {
		categoryComponent = &daemon.ProfilingData{
			Name: category,
		}
		profiling.Components = append(profiling.Components, categoryComponent)
	}

	return RecordDurationComponent(start, categoryComponent, f...)
}

// LogDuration logs the elapsed time since start.
// Use with defer to log the time spent in a function
// If no f is provided, uses the caller.
func LogDuration(start time.Time, f ...any) {
	duration := time.Since(start)

	var pc uintptr
	if len(f) == 0 {
		pc, _, _, _ = runtime.Caller(1)
	} else {
		pc = reflect.ValueOf(f[0]).Pointer()
	}

	name := utils.FunctionName(pc)
	log.Trace().Str("in", name).Msgf("spent %s", duration)
}
