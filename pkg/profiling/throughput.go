package profiling

import (
	"context"
	"time"

	"github.com/cedana/cedana/pkg/keys"
)

type Reference struct {
	Duration time.Duration
	Source   string
	Samples  int
	Key      string
}

func (data *Data) AddIO(n int64) {
	if data != nil {
		data.IO += n
	}
}

// SetReferenceDuration records a controller-derived phase reference. A zero duration leaves the
// profile unchanged.
func SetReferenceDuration(ctx context.Context, duration time.Duration) {
	SetReference(ctx, Reference{Duration: duration})
}

func SetReference(ctx context.Context, reference Reference) {
	data, ok := ctx.Value(keys.PROFILING_CONTEXT_KEY).(*Data)
	if !ok || data == nil || reference.Duration <= 0 {
		return
	}
	data.ReferenceDuration = int64(reference.Duration)
	data.ReferenceSource = reference.Source
	data.ReferenceSamples = reference.Samples
	data.ReferenceKey = reference.Key
}
