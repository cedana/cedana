package types

import (
	"context"
	"sync"

	"github.com/cedana/cedana/pkg/api/daemon"
)

type (
	DumpHandler    func(context.Context, *sync.WaitGroup, *daemon.DumpResp, *daemon.DumpReq) error
	RestoreHandler func(context.Context, context.Context, *sync.WaitGroup, *daemon.RestoreResp, *daemon.RestoreReq) (chan int, error)
	StartHandler   func(context.Context, context.Context, *sync.WaitGroup, *daemon.StartResp, *daemon.StartReq) (chan int, error)
	ManageHandler  func(context.Context, *sync.WaitGroup, *daemon.ManageResp, *daemon.ManageReq) error

	Adapter[H DumpHandler | RestoreHandler | StartHandler | ManageHandler] func(H) H
)

// Adapted takes a Handler and a list of Adapters, and
// returns a new Handler that applies the adapters in order.
func Adapted[H DumpHandler | RestoreHandler | StartHandler | ManageHandler](h H, adapters ...Adapter[H]) H {
	for i := len(adapters) - 1; i >= 0; i-- {
		h = adapters[i](h)
	}
	return h
}
