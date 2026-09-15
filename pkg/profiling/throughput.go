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
type storageTransferContextKey struct{}

func HasData(ctx context.Context) bool {
	_, ok := ctx.Value(keys.PROFILING_CONTEXT_KEY).(*Data)
	return ok
}

func WithThroughputLimit(ctx context.Context, limit ThroughputLimit) context.Context {
	if limit.MaxThroughput <= 0 {
		return ctx
	}
	return context.WithValue(ctx, throughputLimitContextKey{}, limit)
}

func WithStorageTransfer(ctx context.Context) context.Context {
	return context.WithValue(ctx, storageTransferContextKey{}, struct{}{})
}

func AddStorageTransfer(ctx context.Context, n int64) {
	if _, ok := ctx.Value(storageTransferContextKey{}).(struct{}); !ok {
		return
	}
	ApplyContextThroughputLimit(ctx)
	AddIO(ctx, n)
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

func finalizeMeasuredThroughputLimits(data *Data) {
	if data == nil {
		return
	}
	for _, component := range data.Components {
		finalizeMeasuredThroughputLimits(component)
	}
	if data.Tags[ThroughputSourceTag] != "measured" || data.Duration <= 0 || data.IO <= 0 {
		return
	}
	observedThroughput := int64(math.Ceil(float64(data.IO) * float64(time.Second) / float64(data.Duration)))
	if observedThroughput > data.MaxThroughput {
		data.MaxThroughput = observedThroughput
		data.updateMinDuration()
	}
}
