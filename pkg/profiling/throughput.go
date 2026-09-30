package profiling

import (
	"context"
	"time"

	"github.com/cedana/cedana/pkg/keys"
)

func (data *Data) AddIO(n int64) {
	if data != nil {
		data.IO += n
	}
}

// SetReferenceDuration records a controller-derived phase reference. A zero duration leaves the
// profile unchanged.
func SetReferenceDuration(ctx context.Context, duration time.Duration) {
	data, ok := ctx.Value(keys.PROFILING_CONTEXT_KEY).(*Data)
	if !ok || data == nil || duration <= 0 {
		return
	}
	data.ReferenceDuration = int64(duration)
}
