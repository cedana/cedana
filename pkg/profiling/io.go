package profiling

import (
	"context"
	"io"
	"reflect"
	"runtime"
	"time"

	"github.com/cedana/cedana/pkg/keys"
	"github.com/cedana/cedana/pkg/metrics"
	"github.com/cedana/cedana/pkg/utils"
	"github.com/rs/zerolog/log"
	"go.opentelemetry.io/otel"
)

// TODO: Add data to OTel meter as well

// commonWrapper holds common profiling data for all io wrappers.
type commonWrapper struct {
	n    int64
	data *Data
	end  func()
}

// updateIO updates the IO metrics and calls the end function.
func (cw *commonWrapper) updateIO() {
	cw.data.IO += cw.n
	cw.end()
}

type readWriteCloser struct {
	commonWrapper
	w io.ReadWriteCloser
}

func (sw *readWriteCloser) Write(p []byte) (n int, err error) {
	n, err = sw.w.Write(p)
	sw.n += int64(n)
	return n, err
}

func (sw *readWriteCloser) Read(p []byte) (n int, err error) {
	n, err = sw.w.Read(p)
	sw.n += int64(n)
	return n, err
}

func (sw *readWriteCloser) Close() error {
	sw.updateIO()
	return sw.w.Close()
}

type readCloser struct {
	commonWrapper
	rc io.ReadCloser
}

func (prc *readCloser) Read(p []byte) (n int, err error) {
	n, err = prc.rc.Read(p)
	prc.n += int64(n)
	return n, err
}

func (prc *readCloser) Close() error {
	prc.updateIO()
	return prc.rc.Close()
}

type writeCloser struct {
	commonWrapper
	wc io.WriteCloser
}

func (pwc *writeCloser) Write(p []byte) (n int, err error) {
	n, err = pwc.wc.Write(p)
	pwc.n += int64(n)
	return n, err
}

func (pwc *writeCloser) Close() error {
	pwc.updateIO()
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

	if data.Name == "" {
		var pc uintptr
		if len(f) == 0 {
			pc, _, _, _ = runtime.Caller(1)
			data.Name = utils.FunctionName(pc)
		} else if reflect.TypeOf(f[0]).Kind() == reflect.Func {
			pc = reflect.ValueOf(f[0]).Pointer()
			data.Name = utils.FunctionName(pc)
		} else if reflect.TypeOf(f[0]).Kind() == reflect.String {
			data.Name = f[0].(string)
		}
	}

	start := time.Now()
	_, span := otel.Tracer(metrics.TRACER_NAME).Start(ctx, data.Name)

	end := func() {
		duration := time.Since(start)
		span.End()
		data.Duration = duration.Nanoseconds()

		log.Trace().Str("in", data.Name).Msgf("spent %s", duration)
	}

	common := commonWrapper{
		data: data,
		end:  end,
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

	var pc uintptr
	var name string
	if len(f) == 0 {
		pc, _, _, _ = runtime.Caller(1)
		name = utils.FunctionName(pc)
	} else if reflect.TypeOf(f[0]).Kind() == reflect.Func {
		pc = reflect.ValueOf(f[0]).Pointer()
		name = utils.FunctionName(pc)
	} else if reflect.TypeOf(f[0]).Kind() == reflect.String {
		name = f[0].(string)
	}

	component := &Data{Name: name}
	data.Components = append(data.Components, component)

	start := time.Now()
	_, span := otel.Tracer(metrics.TRACER_NAME).Start(ctx, component.Name)

	end := func() {
		duration := time.Since(start)
		span.End()
		component.Duration = duration.Nanoseconds()

		log.Trace().Str("in", component.Name).Msgf("spent %s", duration)
	}

	common := commonWrapper{
		data: component,
		end:  end,
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

	var pc uintptr
	var name string
	if len(f) == 0 {
		pc, _, _, _ = runtime.Caller(1)
		name = utils.FunctionName(pc)
	} else if reflect.TypeOf(f[0]).Kind() == reflect.Func {
		pc = reflect.ValueOf(f[0]).Pointer()
		name = utils.FunctionName(pc)
	} else if reflect.TypeOf(f[0]).Kind() == reflect.String {
		name = f[0].(string)
	}

	childComponent := &Data{Name: name}
	categoryComponent.Components = append(categoryComponent.Components, childComponent)

	start := time.Now()
	_, span := otel.Tracer(metrics.TRACER_NAME).Start(ctx, childComponent.Name)

	end := func() {
		duration := time.Since(start)
		span.End()
		categoryComponent.Duration += duration.Nanoseconds()
		childComponent.Duration = duration.Nanoseconds()

		log.Trace().Str("in", data.Name).Msgf("spent %s", duration)
	}

	common := commonWrapper{
		data: childComponent,
		end:  end,
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

	var pc uintptr
	var name string
	if len(f) == 0 {
		pc, _, _, _ = runtime.Caller(1)
		name = utils.FunctionName(pc)
	} else if reflect.TypeOf(f[0]).Kind() == reflect.Func {
		pc = reflect.ValueOf(f[0]).Pointer()
		name = utils.FunctionName(pc)
	} else if reflect.TypeOf(f[0]).Kind() == reflect.String {
		name = f[0].(string)
	}

	component := &Data{Name: name}
	data.Components = append(data.Components, component)
	component.IO += n

	log.Trace().Str("in", component.Name).Msgf("added %d bytes IO", n)
}
