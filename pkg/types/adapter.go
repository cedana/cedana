package types

import (
	"context"
	"sync"

	"github.com/cedana/cedana/pkg/api/daemon"
	"github.com/cedana/cedana/pkg/criu"
)

type (
	// ServerOpts is intended to be passed by **value** to each handler, so that each handler can modify it
	// before passing it to the next handler in the chain, without affecting the original value.
	ServerOpts struct {
		WG       *sync.WaitGroup
		CRIU     *criu.Criu
		Lifetime context.Context
	}

	// Generic Dump Handler
	Dump func(context.Context, ServerOpts, *daemon.DumpResp, *daemon.DumpReq) error
	// Generic Restore Handler
	Restore func(context.Context, ServerOpts, *daemon.RestoreResp, *daemon.RestoreReq) (chan int, error)
	// Generic Start Handler
	Start func(context.Context, ServerOpts, *daemon.StartResp, *daemon.StartReq) (chan int, error)
	// Generic Manage Handler
	Manage func(context.Context, ServerOpts, *daemon.ManageResp, *daemon.ManageReq) (chan int, error)

	// An adapter is a function that takes a Handler and returns a new Handler
	Adapter[H Dump | Restore | Start | Manage] func(H) H

	// A middleware is simply a chain of adapters
	Middleware[H Dump | Restore | Start | Manage] []Adapter[H]
)

// With is a method on Handler that applies a list of Middleware to the Handler
func (h Dump) With(middleware ...Adapter[Dump]) Dump {
	return adapted(h, middleware...)
}

// With is a method on Handler that applies a list of Middleware to the Handler
func (h Restore) With(middleware ...Adapter[Restore]) Restore {
	return adapted(h, middleware...)
}

// With is a method on Handler that applies a list of Middleware to the Handler
func (h Start) With(middleware ...Adapter[Start]) Start {
	return adapted(h, middleware...)
}

// With is a method on Handler that applies a list of Middleware to the Handler
func (h Manage) With(middleware ...Adapter[Manage]) Manage {
	return adapted(h, middleware...)
}

//////////////////////////
//// Helper Functions ////
//////////////////////////

// Adapted takes a Handler and a list of Adapters, and
// returns a new Handler that applies the adapters in order.
func adapted[H Dump | Restore | Start | Manage](h H, adapters ...Adapter[H]) H {
	for i := len(adapters) - 1; i >= 0; i-- {
		h = adapters[i](h)
	}
	return h
}
