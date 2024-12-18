package types

// Defines the types and functions used to create and manage server handlers, adapters, and middleware.

import (
	"context"
	"sync"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/config"
	"github.com/cedana/cedana/pkg/criu"
	"github.com/cedana/cedana/pkg/plugins"
)

type (
	// ServerOpts is intended to be passed by **value** to each handler, so that each handler can modify it
	// before passing it to the next handler in the chain, without affecting the original value.
	ServerOpts struct {
		WG           *sync.WaitGroup
		CRIU         *criu.Criu
		CRIUCallback *criu.NotifyCallbackMulti
		Plugins      plugins.Manager
		Lifetime     context.Context
		Profiling    *daemon.ProfilingData
	}

	Dump    = Handler[daemon.DumpReq, daemon.DumpResp]
	Restore = Handler[daemon.RestoreReq, daemon.RestoreResp]
	Run     = Handler[daemon.RunReq, daemon.RunResp]

	Handler[REQ, RESP any] func(context.Context, ServerOpts, *RESP, *REQ) (exited chan int, err error)

	// An adapter is a function that takes a Handler and returns a new Handler
	Adapter[H any] func(H) H

	// A middleware is simply a chain of adapters
	Middleware[H any] []Adapter[H]
)

// With applies the given middleware to the handler.
// When profiling is enabled, a timing profiler is added to the middleware chain.
func (h Handler[REQ, RESP]) With(middleware ...Adapter[Handler[REQ, RESP]]) Handler[REQ, RESP] {
	if config.Global.Profiling.Enabled {
		return adaptedWithProfiler(h, Timing, middleware...)
	}
	return adapted(h, middleware...)
}

//////////////////////////
//// Helper Functions ////
//////////////////////////

// Adapted takes a Handler and a list of Adapters, and
// returns a new Handler that applies the adapters in order.
func adapted[REQ, RESP any](h Handler[REQ, RESP], adapters ...Adapter[Handler[REQ, RESP]]) Handler[REQ, RESP] {
	for i := len(adapters) - 1; i >= 0; i-- {
		h = adapters[i](h)
	}
	return h
}

// A profiler can be used to profile each adapter the request
// goes through before reaching the final handler. A profiler is also, itself, an adapter,
// and is inserted in between any two adapters in the middleware chain.
func adaptedWithProfiler[REQ, RESP any](h Handler[REQ, RESP], profiler Adapter[Handler[REQ, RESP]], adapters ...Adapter[Handler[REQ, RESP]]) Handler[REQ, RESP] {
	newMiddleware := make([]Adapter[Handler[REQ, RESP]], 0, len(adapters)*2+1)
	for _, m := range adapters {
		newMiddleware = append(newMiddleware, profiler, m)
	}
	newMiddleware = append(newMiddleware, profiler)

	return adapted(h, newMiddleware...)
}
