package profiling

import (
	"context"
	"math"
	"time"

	"github.com/cedana/cedana/pkg/keys"
)

const (
	ThroughputSourceTag    = "throughput_source"
	ThroughputResourceTag  = "throughput_resource"
	ThroughputDirectionTag = "throughput_direction"
)

type ThroughputLimit struct {
	MaxThroughput int64
	Source        string
	Resource      string
	Direction     string
}

type throughputLimitContextKey struct{}

func WithThroughputLimit(ctx context.Context, limit ThroughputLimit) context.Context {
	if limit.MaxThroughput <= 0 {
		return ctx
	}
	return context.WithValue(ctx, throughputLimitContextKey{}, limit)
}

func ApplyContextThroughputLimit(ctx context.Context) {
	data, ok := ctx.Value(keys.PROFILING_CONTEXT_KEY).(*Data)
	if ok {
		applyContextThroughputLimit(ctx, data)
	}
}

func SetThroughputLimit(ctx context.Context, limit ThroughputLimit) {
	data, ok := ctx.Value(keys.PROFILING_CONTEXT_KEY).(*Data)
	if ok {
		setThroughputLimit(data, limit)
	}
}

func applyContextThroughputLimit(ctx context.Context, data *Data) {
	limit, ok := ctx.Value(throughputLimitContextKey{}).(ThroughputLimit)
	if ok {
		setThroughputLimit(data, limit)
	}
}

func setThroughputLimit(data *Data, limit ThroughputLimit) {
	if data == nil || limit.MaxThroughput <= 0 {
		return
	}
	data.MaxThroughput = limit.MaxThroughput
	if limit.Source != "" || limit.Resource != "" || limit.Direction != "" {
		if data.Tags == nil {
			data.Tags = make(map[string]string)
		}
		if limit.Source != "" {
			data.Tags[ThroughputSourceTag] = limit.Source
		}
		if limit.Resource != "" {
			data.Tags[ThroughputResourceTag] = limit.Resource
		}
		if limit.Direction != "" {
			data.Tags[ThroughputDirectionTag] = limit.Direction
		}
	}
	data.updateMinDuration()
}

func (data *Data) AddIO(n int64) {
	if data == nil {
		return
	}
	data.IO += n
	data.updateMinDuration()
}

func (data *Data) updateMinDuration() {
	if data.IO <= 0 || data.MaxThroughput <= 0 {
		data.MinDuration = 0
		return
	}

	minDuration := math.Ceil(float64(data.IO) * float64(time.Second) / float64(data.MaxThroughput))
	if minDuration >= float64(math.MaxInt64) {
		data.MinDuration = math.MaxInt64
		return
	}
	data.MinDuration = int64(minDuration)
}
