package profiling

import (
	"context"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/cedana/cedana/pkg/keys"
)

const (
	ThroughputSourceTag    = "throughput_source"
	ThroughputResourceTag  = "throughput_resource"
	ThroughputDirectionTag = "throughput_direction"
	ObservedResourceTag    = "observed_resource"
	ObservedDirectionTag   = "observed_direction"
	ObservedShapeTag       = "observed_shape"
)

type ThroughputLimit struct {
	MaxThroughput int64
	Source        string
	Resource      string
	Direction     string
}

type throughputLimitContextKey struct{}
type storageTransferContextKey struct{}
type observedThroughputContextKey struct{}
type numaThroughputContextKey struct{}

// NUMAThroughputLimiter provides a measured host-memory limit for a process.
type NUMAThroughputLimiter interface {
	LimitForPID(ctx context.Context, pid uint32) (int64, string, bool)
}

type ObservedThroughputCache struct {
	mu       sync.Mutex
	entries  map[observedThroughputKey]observedThroughputEntry
	capacity int
	ttl      time.Duration
}

type observedThroughputContext struct {
	cache     *ObservedThroughputCache
	resource  string
	direction string
}

type observedThroughputKey struct {
	resource  string
	direction string
	shape     string
}

type observedThroughputEntry struct {
	rate      int64
	expiresAt time.Time
}

func NewObservedThroughputCache(capacity int, ttl time.Duration) *ObservedThroughputCache {
	if capacity <= 0 {
		capacity = 64
	}
	if ttl <= 0 {
		ttl = 30 * time.Minute
	}
	return &ObservedThroughputCache{
		entries:  make(map[observedThroughputKey]observedThroughputEntry),
		capacity: capacity,
		ttl:      ttl,
	}
}

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

func WithObservedThroughput(ctx context.Context, cache *ObservedThroughputCache, resource, direction string) context.Context {
	if cache == nil || resource == "" || direction == "" {
		return ctx
	}
	return context.WithValue(ctx, observedThroughputContextKey{}, observedThroughputContext{
		cache:     cache,
		resource:  resource,
		direction: direction,
	})
}

// WithNUMAThroughput makes process-specific NUMA limits available to profile components.
func WithNUMAThroughput(ctx context.Context, limiter NUMAThroughputLimiter) context.Context {
	if limiter == nil {
		return ctx
	}
	return context.WithValue(ctx, numaThroughputContextKey{}, limiter)
}

// NUMAThroughputLimit returns the measured host-memory limit for a profiled process.
func NUMAThroughputLimit(ctx context.Context, pid uint32) *ThroughputLimit {
	limiter, ok := ctx.Value(numaThroughputContextKey{}).(NUMAThroughputLimiter)
	if !ok {
		return nil
	}
	throughput, resource, ok := limiter.LimitForPID(ctx, pid)
	if !ok || throughput <= 0 {
		return nil
	}
	return &ThroughputLimit{
		MaxThroughput: throughput,
		Source:        "measured",
		Resource:      resource,
	}
}

func AddStorageTransfer(ctx context.Context, n int64) {
	if _, ok := ctx.Value(storageTransferContextKey{}).(struct{}); !ok {
		return
	}
	ApplyContextObservedThroughput(ctx)
	ApplyContextThroughputLimit(ctx)
	AddIO(ctx, n)
}

func ApplyContextObservedThroughput(ctx context.Context) {
	data, ok := ctx.Value(keys.PROFILING_CONTEXT_KEY).(*Data)
	if ok {
		applyContextObservedThroughput(ctx, data)
	}
}

func RecordObservedThroughput(ctx context.Context) {
	observed, ok := ctx.Value(observedThroughputContextKey{}).(observedThroughputContext)
	if !ok || observed.cache == nil {
		return
	}
	data, ok := ctx.Value(keys.PROFILING_CONTEXT_KEY).(*Data)
	if !ok {
		return
	}
	recordObservedThroughput(observed.cache, data)
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

func applyContextObservedThroughput(ctx context.Context, data *Data) {
	observed, ok := ctx.Value(observedThroughputContextKey{}).(observedThroughputContext)
	if !ok || data == nil {
		return
	}
	key := observedThroughputKey{
		resource:  observed.resource,
		direction: observed.direction,
		shape:     data.Name,
	}
	if data.Tags == nil {
		data.Tags = make(map[string]string)
	}
	data.Tags[ObservedResourceTag] = key.resource
	data.Tags[ObservedDirectionTag] = key.direction
	data.Tags[ObservedShapeTag] = key.shape
	if rate, found := observed.cache.lookup(key); found {
		data.ObservedThroughput = rate
		data.updateObservedDuration()
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
	data.updateObservedDuration()
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

func (data *Data) updateObservedDuration() {
	if data.IO <= 0 || data.ObservedThroughput <= 0 {
		data.ObservedDuration = 0
		return
	}

	observedDuration := math.Ceil(float64(data.IO) * float64(time.Second) / float64(data.ObservedThroughput))
	if observedDuration >= float64(math.MaxInt64) {
		data.ObservedDuration = math.MaxInt64
		return
	}
	data.ObservedDuration = int64(observedDuration)
}

func recordObservedThroughput(cache *ObservedThroughputCache, data *Data) {
	if data == nil {
		return
	}
	if data.Tags != nil {
		key := observedThroughputKey{
			resource:  data.Tags[ObservedResourceTag],
			direction: data.Tags[ObservedDirectionTag],
			shape:     data.Tags[ObservedShapeTag],
		}
		if key.resource != "" && key.direction != "" && key.shape != "" && data.IO > 0 && data.Duration > 0 {
			cache.record(key, data.IO, data.Duration)
		}
	}
	for _, component := range data.Components {
		recordObservedThroughput(cache, component)
	}
}

func (cache *ObservedThroughputCache) lookup(key observedThroughputKey) (int64, bool) {
	cache.mu.Lock()
	defer cache.mu.Unlock()

	entry, found := cache.entries[key]
	if !found {
		return 0, false
	}
	if time.Now().After(entry.expiresAt) {
		delete(cache.entries, key)
		return 0, false
	}
	return entry.rate, true
}

func (cache *ObservedThroughputCache) record(key observedThroughputKey, bytes, duration int64) {
	if bytes <= 0 || duration <= 0 {
		return
	}
	rate := int64(float64(bytes) * float64(time.Second) / float64(duration))
	if rate <= 0 {
		return
	}

	cache.mu.Lock()
	defer cache.mu.Unlock()

	now := time.Now()
	for existingKey, entry := range cache.entries {
		if now.After(entry.expiresAt) {
			delete(cache.entries, existingKey)
		}
	}
	if entry, found := cache.entries[key]; !found || rate > entry.rate {
		cache.entries[key] = observedThroughputEntry{rate: rate, expiresAt: now.Add(cache.ttl)}
	}
	if len(cache.entries) <= cache.capacity {
		return
	}

	keys := make([]observedThroughputKey, 0, len(cache.entries))
	for existingKey := range cache.entries {
		keys = append(keys, existingKey)
	}
	sort.Slice(keys, func(i, j int) bool {
		return cache.entries[keys[i]].expiresAt.Before(cache.entries[keys[j]].expiresAt)
	})
	for len(cache.entries) > cache.capacity {
		delete(cache.entries, keys[0])
		keys = keys[1:]
	}
}
