package types

import (
	"context"
	"sync"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/criu"
	"github.com/cedana/cedana/pkg/plugins"
)

type (
	// ServerOpts is intended to be passed by **value** to each handler, so that each handler can modify it
	// before passing it to the next handler in the chain, without affecting the original value.
	ServerOpts struct {
		WG       *sync.WaitGroup
		CRIU     *criu.Criu
		Plugins  plugins.Manager
		Lifetime context.Context
	}

	// Generic handler type
	Handler[RSP, REQ any] func(context.Context, ServerOpts, RSP, REQ) (exited chan int, err error)

	Dump    Handler[*daemon.DumpResp, *daemon.DumpReq]
	Restore Handler[*daemon.RestoreResp, *daemon.RestoreReq]
	Start   Handler[*daemon.StartResp, *daemon.StartReq]
	Manage  Handler[*daemon.ManageResp, *daemon.ManageReq]

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
