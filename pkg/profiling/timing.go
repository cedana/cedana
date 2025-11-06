package profiling

import (
	"context"
	"fmt"
	"reflect"
	"runtime"
	"time"

	"github.com/cedana/cedana/pkg/keys"
	"github.com/cedana/cedana/pkg/metrics"
	"github.com/cedana/cedana/pkg/utils"
	"github.com/rs/zerolog/log"
	"go.opentelemetry.io/otel"
)

// StartTiming starts a timer and returns a function that should be called to end the timer.
// Uses the profiling data in ctx to store the data, and returns a child context that should be used by the children of
// the function. If no f is provided, uses the caller to get the function name.
// This should only be called for the top most leader in the tree.
func StartTiming(ctx context.Context, f ...any) (childCtx context.Context, end func()) {
	var data *Data
	data, ok := ctx.Value(keys.PROFILING_CONTEXT_KEY).(*Data)
	if !ok {
		data = &Data{}
	}

	data.Name = getName(f...)

	start := time.Now()
	childCtx, span := otel.Tracer(metrics.TRACER_NAME).Start(ctx, data.Name)

	end = func() {
		duration := time.Since(start)
		span.End()
		data.Duration = duration.Nanoseconds()

		log.Trace().Str("in", data.Name).Msgf("spent %s", duration)
	}

	childCtx = context.WithValue(childCtx, keys.PROFILING_CONTEXT_KEY, data)

	return childCtx, end
}

// StartTimingComponent starts a timer and returns a function that should be called to end the timer.
// Unlike StartTiming, this adds the data as a new component of the current data in ctx.
// Returns childCtx, which should be used by the children of the new component.
// If no data found in passed ctx, just returns noops, as the parent is not being profiled.
func StartTimingComponent(ctx context.Context, f ...any) (childCtx context.Context, end func()) {
	var data *Data
	data, ok := ctx.Value(keys.PROFILING_CONTEXT_KEY).(*Data)
	if !ok {
		return ctx, func() {}
	}

	component := &Data{Name: getName(f...)}
	data.Components = append(data.Components, component)

	start := time.Now()
	childCtx, span := otel.Tracer(metrics.TRACER_NAME).Start(ctx, component.Name)
	childCtx = context.WithValue(childCtx, keys.PROFILING_CONTEXT_KEY, component)

	end = func() {
		duration := time.Since(start)
		span.End()
		component.Duration = duration.Nanoseconds()

		log.Trace().Str("in", component.Name).Msgf("spent %s", duration)
	}

	return childCtx, end
}

func StartTimingParallelComponent(ctx context.Context, f ...any) (childCtx context.Context, end func()) {
	var data *Data
	data, ok := ctx.Value(keys.PROFILING_CONTEXT_KEY).(*Data)
	if !ok {
		return ctx, func() {}
	}

	component := &Data{Name: getName(f...), Parallel: true}
	data.Components = append(data.Components, component)

	start := time.Now()
	childCtx, span := otel.Tracer(metrics.TRACER_NAME).Start(ctx, component.Name)
	childCtx = context.WithValue(childCtx, keys.PROFILING_CONTEXT_KEY, component)

	end = func() {
		duration := time.Since(start)
		span.End()
		component.Duration = duration.Nanoseconds()

		log.Trace().Str("in", component.Name).Msgf("spent %s", duration)
	}

	return childCtx, end
}

// StartTimingCategory starts a timer and returns a function that should be called to end the timer.
// Instead of directly inserting a component like StartTimingComponent, this adds the data as a child component
// to an empty component (category component) whose name is matching the category provided.
// Returns childCtx, which should be used by the children of the new component.
// If no data found in passed ctx, just returns noops, as the parent is not being profiled.
func StartTimingCategory(ctx context.Context, category string, f ...any) (childCtx context.Context, end func()) {
	var data *Data
	data, ok := ctx.Value(keys.PROFILING_CONTEXT_KEY).(*Data)
	if !ok {
		return ctx, func() {}
	}

	var categoryComponent *Data
	// Add to existing category component if it exists
	for _, component := range data.Components {
		if component.Name == category {
			categoryComponent = component
			break
		}
	}
	if categoryComponent == nil {
		categoryComponent = &Data{
			Name: category,
		}
		data.Components = append(data.Components, categoryComponent)
	}

	childComponent := &Data{Name: getName(f...)}
	categoryComponent.Components = append(categoryComponent.Components, childComponent)

	start := time.Now()
	childCtx, span := otel.Tracer(metrics.TRACER_NAME).Start(ctx, childComponent.Name)

	end = func() {
		duration := time.Since(start)
		span.End()
		categoryComponent.Duration += duration.Nanoseconds()
		childComponent.Duration = duration.Nanoseconds()

		log.Trace().Str("in", data.Name).Msgf("spent %s", duration)
	}

	childCtx = context.WithValue(childCtx, keys.PROFILING_CONTEXT_KEY, childComponent)

	return childCtx, end
}

func StartTimingParallelCategory(ctx context.Context, category string, f ...any) (childCtx context.Context, end func()) {
	var data *Data
	data, ok := ctx.Value(keys.PROFILING_CONTEXT_KEY).(*Data)
	if !ok {
		return ctx, func() {}
	}

	var categoryComponent *Data
	// Add to existing category component if it exists
	for _, component := range data.Components {
		if component.Name == category {
			categoryComponent = component
			break
		}
	}
	if categoryComponent == nil {
		categoryComponent = &Data{
			Name: category,
		}
		data.Components = append(data.Components, categoryComponent)
	}

	childComponent := &Data{Name: getName(f...), Parallel: true}
	categoryComponent.Components = append(categoryComponent.Components, childComponent)

	start := time.Now()
	childCtx, span := otel.Tracer(metrics.TRACER_NAME).Start(ctx, childComponent.Name)

	end = func() {
		duration := time.Since(start)
		span.End()
		// Don't count parallel durations towards the category total
		childComponent.Duration = duration.Nanoseconds()

		log.Trace().Str("in", data.Name).Msgf("spent %s", duration)
	}

	childCtx = context.WithValue(childCtx, keys.PROFILING_CONTEXT_KEY, childComponent)

	return childCtx, end
}

// AddTimingComponent is just like StartTimingComponent, but for adding a duration directly.
func AddTimingComponent(ctx context.Context, duration time.Duration, f ...any) (childCtx context.Context) {
	var data *Data
	data, ok := ctx.Value(keys.PROFILING_CONTEXT_KEY).(*Data)
	if !ok {
		return ctx
	}

	component := &Data{Name: getName(f...), Duration: duration.Nanoseconds()}
	data.Components = append(data.Components, component)

	childCtx = context.WithValue(ctx, keys.PROFILING_CONTEXT_KEY, component)
	log.Trace().Str("in", component.Name).Msgf("spent %s", duration)

	return childCtx
}

func AddTimingParallelComponent(ctx context.Context, duration time.Duration, f ...any) (childCtx context.Context) {
	var data *Data
	data, ok := ctx.Value(keys.PROFILING_CONTEXT_KEY).(*Data)
	if !ok {
		return ctx
	}

	component := &Data{Name: getName(f...), Duration: duration.Nanoseconds(), Parallel: true}
	data.Components = append(data.Components, component)

	childCtx = context.WithValue(ctx, keys.PROFILING_CONTEXT_KEY, component)
	log.Trace().Str("in", component.Name).Msgf("spent %s", duration)

	return childCtx
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

// DurationStr converts the duration to the specified precision.
func DurationStr(d time.Duration, precision string) string {
	switch precision {
	case "s":
		return fmt.Sprintf("%gs", float64(d.Nanoseconds())/1e9)
	case "ms":
		return fmt.Sprintf("%gms", float64(d.Nanoseconds())/1e6)
	case "us":
		return fmt.Sprintf("%gus", float64(d.Nanoseconds())/1e3)
	case "ns":
		return fmt.Sprintf("%gns", float64(d.Nanoseconds()))
	}

	// auto
	return d.String()
}

///////////////
/// HELPERS ///
///////////////

func getName(f ...any) string {
	var name string
	if len(f) == 0 {
		pc, _, _, _ := runtime.Caller(2)
		name = utils.FunctionName(pc)
	} else {
		var tags []string
		for _, f := range f {
			if reflect.TypeOf(f).Kind() == reflect.Func {
				pc := reflect.ValueOf(f).Pointer()
				tags = append(tags, utils.FunctionName(pc))
			} else if reflect.TypeOf(f).Kind() == reflect.String {
				tags = append(tags, f.(string))
			}
		}
		name = tags[0]
		if len(tags) > 1 {
			name = fmt.Sprintf("%s (%s)", name, utils.StrList(tags[1:]))
		}
	}
	return name
}
