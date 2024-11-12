package types

import (
	"context"
	"sync"

	"github.com/cedana/cedana/pkg/api/daemon"
	"github.com/cedana/cedana/pkg/criu"
)

type (
	Handler[H Dump | Restore | Start | Manage] struct {
		WG       *sync.WaitGroup
		CRIU     *criu.Criu
		Lifetime context.Context

		Handle H
	}

	Dump    func(context.Context, *daemon.DumpResp, *daemon.DumpReq) error
	Restore func(context.Context, *daemon.RestoreResp, *daemon.RestoreReq) (chan int, error)
	Start   func(context.Context, *daemon.StartResp, *daemon.StartReq) (chan int, error)
	Manage  func(context.Context, *daemon.ManageResp, *daemon.ManageReq) (chan int, error)

	// An adapter is a function that takes a Handler and returns a new Handler
	Adapter[H Dump | Restore | Start | Manage] func(Handler[H]) Handler[H]

	// A middleware is simply a chain of adapters
	Middleware[H Dump | Restore | Start | Manage] []Adapter[H]
)

func (h Handler[H]) With(
	lifetime context.Context,
	wg *sync.WaitGroup,
	criu *criu.Criu,
	middleware ...Adapter[H],
) Handler[H] {
	return adapted(h, middleware...)
}

//////////////////////////
//// Helper Functions ////
//////////////////////////

// Adapted takes a Handler and a list of Adapters, and
// returns a new Handler that applies the adapters in order.
func adapted[H Dump | Restore | Start | Manage](h Handler[H], adapters ...Adapter[H]) Handler[H] {
	for i := len(adapters) - 1; i >= 0; i-- {
		h = adapters[i](h)
	}
	return h
}
