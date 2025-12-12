package profiling

import (
	"context"
	"io"
	"time"

	"github.com/cedana/cedana/pkg/keys"
	"github.com/cedana/cedana/pkg/metrics"
	"github.com/rs/zerolog/log"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
)

// TODO: Add data to OTel meter as well

// commonWrapper holds common profiling data for all io wrappers.
type commonWrapper struct {
	data  *Data
	start func()
	end   func(n *int)
}

type readWriteCloser struct {
	commonWrapper
	w io.ReadWriteCloser
}

func (sw *readWriteCloser) Write(p []byte) (n int, err error) {
	sw.start()
	defer sw.end(&n)
	return sw.w.Write(p)
}

func (sw *readWriteCloser) Read(p []byte) (n int, err error) {
	sw.start()
	defer sw.end(&n)
	return sw.w.Read(p)
}

func (sw *readWriteCloser) Close() error {
	return sw.w.Close()
}

type readCloser struct {
	commonWrapper
	rc io.ReadCloser
}

func (prc *readCloser) Read(p []byte) (n int, err error) {
	prc.start()
	defer prc.end(&n)
	return prc.rc.Read(p)
}

func (prc *readCloser) Close() error {
	return prc.rc.Close()
}

type writeCloser struct {
	commonWrapper
	wc io.WriteCloser
}

func (pwc *writeCloser) Write(p []byte) (n int, err error) {
	pwc.start()
	defer pwc.end(&n)
	return pwc.wc.Write(p)
}

func (pwc *writeCloser) Close() error {
	return pwc.wc.Close()
}

// IO wraps an io.ReadCloser, io.WriteCloser, or io.ReadWriteCloser
// to profile its I/O operations as part of profiling data of current context.
// The returned type will be the same as the input type.
func IO[T any](ctx context.Context, w T, f ...any) T {
	var data *Data
	data, ok := ctx.Value(keys.PROFILING_CONTEXT_KEY).(*Data)
	if !ok {
		data = &Data{}
	}

	data.Name = getName(f...)

	var span trace.Span
	var beginning time.Time

	start := func() {
		_, span = otel.Tracer(metrics.TRACER_NAME).Start(ctx, data.Name)
		beginning = time.Now()
	}

	end := func(n *int) {
		data.Duration += time.Since(beginning).Nanoseconds()
		data.IO += int64(*n)
		span.End()
	}

	common := commonWrapper{
		data:  data,
		start: start,
		end:   end,
	}

	switch v := any(w).(type) {
	case io.ReadWriteCloser:
		return any(&readWriteCloser{commonWrapper: common, w: v}).(T)
	case io.ReadCloser:
		return any(&readCloser{commonWrapper: common, rc: v}).(T)
	case io.WriteCloser:
		return any(&writeCloser{commonWrapper: common, wc: v}).(T)
	default:
		log.Trace().Str("in", data.Name).Msgf("unsupported io type %T", w)
		return w
	}
}

// IO wraps an io.ReadCloser, io.WriteCloser, or io.ReadWriteCloser
// to profile its I/O operations as a component of the profiling data of the current context.
// The returned type will be the same as the input type.
func IOComponent[T any](ctx context.Context, w T, f ...any) T {
	var data *Data
	data, ok := ctx.Value(keys.PROFILING_CONTEXT_KEY).(*Data)
	if !ok {
		return w
	}

	component := &Data{Name: getName(f...)}
	data.Components = append(data.Components, component)

	var span trace.Span
	var beginning time.Time

	start := func() {
		_, span = otel.Tracer(metrics.TRACER_NAME).Start(ctx, component.Name)
		beginning = time.Now()
	}

	end := func(n *int) {
		component.Duration += time.Since(beginning).Nanoseconds()
		component.IO += int64(*n)
		span.End()
	}

	common := commonWrapper{
		data:  component,
		start: start,
		end:   end,
	}

	switch v := any(w).(type) {
	case io.ReadWriteCloser:
		return any(&readWriteCloser{commonWrapper: common, w: v}).(T)
	case io.ReadCloser:
		return any(&readCloser{commonWrapper: common, rc: v}).(T)
	case io.WriteCloser:
		return any(&writeCloser{commonWrapper: common, wc: v}).(T)
	default:
		log.Trace().Str("in", component.Name).Msgf("unsupported io type %T", w)
		return w
	}
}

// IOParallelComponent is same as IOComponent but marks the component as parallel.
// Parallel components' durations are not counted towards their parent's duration.
// However, their I/O is still counted towards total.
func IOParallelComponent[T any](ctx context.Context, w T, f ...any) T {
	var data *Data
	data, ok := ctx.Value(keys.PROFILING_CONTEXT_KEY).(*Data)
	if !ok {
		return w
	}

	component := &Data{Name: getName(f...), Parallel: true}
	data.Components = append(data.Components, component)

	var span trace.Span
	var beginning time.Time

	start := func() {
		_, span = otel.Tracer(metrics.TRACER_NAME).Start(ctx, component.Name)
		beginning = time.Now()
	}

	end := func(n *int) {
		component.Duration += time.Since(beginning).Nanoseconds()
		component.IO += int64(*n)
		span.End()
	}

	common := commonWrapper{
		data:  component,
		start: start,
		end:   end,
	}

	switch v := any(w).(type) {
	case io.ReadWriteCloser:
		return any(&readWriteCloser{commonWrapper: common, w: v}).(T)
	case io.ReadCloser:
		return any(&readCloser{commonWrapper: common, rc: v}).(T)
	case io.WriteCloser:
		return any(&writeCloser{commonWrapper: common, wc: v}).(T)
	default:
		log.Trace().Str("in", component.Name).Msgf("unsupported io type %T", w)
		return w
	}
}

// IORedundantComponent is same as IOComponent but marks the component as redundant.
// Redundant components are not counted towards their parent's duration or I/O.
func IORedundantComponent[T any](ctx context.Context, w T, f ...any) T {
	var data *Data
	data, ok := ctx.Value(keys.PROFILING_CONTEXT_KEY).(*Data)
	if !ok {
		return w
	}

	component := &Data{Name: getName(f...), Redundant: true}
	data.Components = append(data.Components, component)

	var span trace.Span
	var beginning time.Time

	start := func() {
		_, span = otel.Tracer(metrics.TRACER_NAME).Start(ctx, component.Name)
		beginning = time.Now()
	}

	end := func(n *int) {
		component.Duration += time.Since(beginning).Nanoseconds()
		component.IO += int64(*n)
		span.End()
	}

	common := commonWrapper{
		data:  component,
		start: start,
		end:   end,
	}

	switch v := any(w).(type) {
	case io.ReadWriteCloser:
		return any(&readWriteCloser{commonWrapper: common, w: v}).(T)
	case io.ReadCloser:
		return any(&readCloser{commonWrapper: common, rc: v}).(T)
	case io.WriteCloser:
		return any(&writeCloser{commonWrapper: common, wc: v}).(T)
	default:
		log.Trace().Str("in", component.Name).Msgf("unsupported io type %T", w)
		return w
	}
}

// IOCategory wraps an io.ReadCloser, io.WriteCloser, or io.ReadWriteCloser
// to profile its I/O operations under a specific category in the profiling data of the current context.
// The returned type will be the same as the input type.
func IOCategory[T any](ctx context.Context, w T, category string, f ...any) T {
	var data *Data
	data, ok := ctx.Value(keys.PROFILING_CONTEXT_KEY).(*Data)
	if !ok {
		return w
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

	var span trace.Span
	var beginning time.Time

	start := func() {
		_, span = otel.Tracer(metrics.TRACER_NAME).Start(ctx, childComponent.Name)
		beginning = time.Now()
	}

	end := func(n *int) {
		duration := time.Since(beginning)
		categoryComponent.Duration += duration.Nanoseconds()
		childComponent.Duration += duration.Nanoseconds()
		childComponent.IO += int64(*n)
		span.End()
	}

	common := commonWrapper{
		data:  childComponent,
		start: start,
		end:   end,
	}

	switch v := any(w).(type) {
	case io.ReadWriteCloser:
		return any(&readWriteCloser{commonWrapper: common, w: v}).(T)
	case io.ReadCloser:
		return any(&readCloser{commonWrapper: common, rc: v}).(T)
	case io.WriteCloser:
		return any(&writeCloser{commonWrapper: common, wc: v}).(T)
	default:
		log.Trace().Str("in", category).Msgf("unsupported io type %T", w)
		return w
	}
}

// IOParallelCategory is same as IOCategory but marks the component as parallel.
// Parallel components' durations are not counted towards their parent's duration.
// However, their I/O is still counted towards total.
func IOParallelCategory[T any](ctx context.Context, w T, category string, f ...any) T {
	var data *Data
	data, ok := ctx.Value(keys.PROFILING_CONTEXT_KEY).(*Data)
	if !ok {
		return w
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

	var span trace.Span
	var beginning time.Time

	start := func() {
		_, span = otel.Tracer(metrics.TRACER_NAME).Start(ctx, childComponent.Name)
		beginning = time.Now()
	}

	end := func(n *int) {
		// Don't count parallel durations towards the category total
		childComponent.Duration = time.Since(beginning).Nanoseconds()
		childComponent.IO += int64(*n)
		span.End()
	}

	common := commonWrapper{
		data:  childComponent,
		start: start,
		end:   end,
	}

	switch v := any(w).(type) {
	case io.ReadWriteCloser:
		return any(&readWriteCloser{commonWrapper: common, w: v}).(T)
	case io.ReadCloser:
		return any(&readCloser{commonWrapper: common, rc: v}).(T)
	case io.WriteCloser:
		return any(&writeCloser{commonWrapper: common, wc: v}).(T)
	default:
		log.Trace().Str("in", category).Msgf("unsupported io type %T", w)
		return w
	}
}

// IORedundantCategory is same as IOCategory but marks the component as redundant.
// Redundant components are not counted towards their parent's duration or I/O.
func IORedundantCategory[T any](ctx context.Context, w T, category string, f ...any) T {
	var data *Data
	data, ok := ctx.Value(keys.PROFILING_CONTEXT_KEY).(*Data)
	if !ok {
		return w
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

	childComponent := &Data{Name: getName(f...), Redundant: true}
	categoryComponent.Components = append(categoryComponent.Components, childComponent)

	var span trace.Span
	var beginning time.Time

	start := func() {
		_, span = otel.Tracer(metrics.TRACER_NAME).Start(ctx, childComponent.Name)
		beginning = time.Now()
	}

	end := func(n *int) {
		// Don't count parallel durations towards the category total
		childComponent.Duration = time.Since(beginning).Nanoseconds()
		childComponent.IO += int64(*n)
		span.End()
	}

	common := commonWrapper{
		data:  childComponent,
		start: start,
		end:   end,
	}

	switch v := any(w).(type) {
	case io.ReadWriteCloser:
		return any(&readWriteCloser{commonWrapper: common, w: v}).(T)
	case io.ReadCloser:
		return any(&readCloser{commonWrapper: common, rc: v}).(T)
	case io.WriteCloser:
		return any(&writeCloser{commonWrapper: common, wc: v}).(T)
	default:
		log.Trace().Str("in", category).Msgf("unsupported io type %T", w)
		return w
	}
}

// AddIO adds n bytes to the IO metric in the profiling data stored in the context.
func AddIO(ctx context.Context, n int64) {
	data, ok := ctx.Value(keys.PROFILING_CONTEXT_KEY).(*Data)
	if !ok {
		return
	}
	data.IO += n
}

func AddIOComponent(ctx context.Context, n int64, f ...any) {
	var data *Data
	data, ok := ctx.Value(keys.PROFILING_CONTEXT_KEY).(*Data)
	if !ok {
		return
	}

	component := &Data{Name: getName(f...)}
	data.Components = append(data.Components, component)
	component.IO += n
}

func AddIOCategory(ctx context.Context, n int64, category string, f ...any) {
	var data *Data
	data, ok := ctx.Value(keys.PROFILING_CONTEXT_KEY).(*Data)
	if !ok {
		return
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
	childComponent.IO += n
}
