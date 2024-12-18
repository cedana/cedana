package utils

import (
	"reflect"
	"runtime"
	"time"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/rs/zerolog/log"
)

// RecordDuration records the elapsed time since start into the profiling data.
// Use with defer to record the time spent in a function.
// If no f is provided, uses the caller.
func RecordDuration(start time.Time, profiling *daemon.ProfilingData, f ...any) {
	duration := time.Since(start)

	var pc uintptr
	if len(f) == 0 {
		pc, _, _, _ = runtime.Caller(1)
	} else {
		pc = reflect.ValueOf(f[0]).Pointer()
	}

	profiling.Duration = duration.Nanoseconds()
	profiling.Name = FunctionName(pc)

	log.Trace().Str("in", profiling.Name).Msgf("spent %s", duration)
}

// RecordComponentDuration records the elapsed time since start into the profiling data.
// Unlike RecordDuration, this adds the data as a new component of the profiling data.
func RecordComponentDuration(start time.Time, profiling *daemon.ProfilingData, f ...any) {
	duration := time.Since(start)

	var pc uintptr
	if len(f) == 0 {
		pc, _, _, _ = runtime.Caller(1)
	} else {
		pc = reflect.ValueOf(f[0]).Pointer()
	}

	name := FunctionName(pc)

	profiling.Duration += duration.Nanoseconds()
	profiling.Components = append(profiling.Components, &daemon.ProfilingData{
		Name:     name,
		Duration: duration.Nanoseconds(),
	})

	log.Trace().Str("in", name).Msgf("spent %s", duration)
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

	name := FunctionName(pc)
	log.Trace().Str("in", name).Msgf("spent %s", duration)
}

// FlattenProfilingData flattens the profiling data into a single list of components.
// This is such that the duration of each component is purely the time spent in that component
// excluding the time spent in its children.
func FlattenProfilingData(data *daemon.ProfilingData) {
	length := len(data.Components)

	for i := 0; i < length; i++ {
		component := data.Components[i]

		data.Duration -= component.Duration

		FlattenProfilingData(component)

		data.Components = append(data.Components, component.Components...)
		component.Components = nil

	}

	// If the data has no duration, it is just a category wrapper for its components.
	// so we append its name to the name of its children.

	if data.Duration == 0 && data.Name != "" {
		for _, component := range data.Components {
			component.Name = data.Name + ":" + component.Name
		}
	}
}
