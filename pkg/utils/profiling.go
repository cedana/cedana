package utils

import (
	"time"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/rs/zerolog/log"
)

// LogElapsed logs the elapsed time since start.
// Use with defer to log the time spent in a function
func LogElapsed(start time.Time, name string) {
	elapsed := time.Since(start)
	log.Trace().Str("in", name).Msgf("spent %s", elapsed)
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
}
