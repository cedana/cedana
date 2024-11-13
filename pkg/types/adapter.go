package types

import (
	"context"
	"sync"

	"github.com/cedana/cedana/pkg/api/daemon"
	"github.com/cedana/cedana/pkg/criu"
)

type (
	Handler[H Dump | Restore | Start | Manage] struct {
		// Private methods. Use getters to access them.
		// This is to ensure these never get overridden.
		wg   *sync.WaitGroup
		criu *criu.Criu

		// Available methods. Can be overridden by an adapter.
		Lifetime context.Context
		Handle   H
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

// NewHandler returns a new Handler with the given lifetime, waitgroup, and criu
func NewHandler[H Dump | Restore | Start | Manage](wg *sync.WaitGroup, criu *criu.Criu) Handler[H] {
	return Handler[H]{
		wg:   wg,
		criu: criu,
	}
}

func (h Handler[H]) GetWG() *sync.WaitGroup {
	return h.wg
}

func (h Handler[H]) GetCRIU() *criu.Criu {
	return h.criu
}

func (h Handler[H]) With(middleware ...Adapter[H]) Handler[H] {
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
