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

// SetMinDuration records a reference derived by a controller that has modeled the phase's
// internal data path. It must not be recomputed from the parent row's aggregate I/O.
func SetMinDuration(ctx context.Context, duration time.Duration) {
	data, ok := ctx.Value(keys.PROFILING_CONTEXT_KEY).(*Data)
	if !ok || data == nil || duration <= 0 {
		return
	}
	data.MinDuration = int64(duration)
}
