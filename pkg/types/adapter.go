package types

import (
	"context"

	"github.com/cedana/cedana/pkg/api/daemon"
)

type (
	DumpHandler    func(context.Context, *daemon.DumpResp, *daemon.DumpReq) error
	RestoreHandler func(context.Context, *daemon.RestoreResp, *daemon.RestoreReq) error

	Adapter[H DumpHandler | RestoreHandler] func(H) H
)

// Adapted takes a Handler and a list of Adapters, and
// returns a new Handler that applies the adapters in order.
func Adapted[H DumpHandler | RestoreHandler](h H, adapters ...Adapter[H]) H {
	for i := len(adapters) - 1; i >= 0; i-- {
		h = adapters[i](h)
	}
	return h
}
