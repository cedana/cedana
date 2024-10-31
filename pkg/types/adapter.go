package types

import (
	"context"

	"github.com/cedana/cedana/pkg/api/daemon"
)

type (
	DumpHandler    func(context.Context, *daemon.DumpResp, *daemon.DumpReq) error
	RestoreHandler func(context.Context, *daemon.RestoreResp, *daemon.RestoreReq) error
)

type (
	DumpAdapter    func(DumpHandler) DumpHandler
	RestoreAdapter func(RestoreHandler) RestoreHandler
)

// AdaptedDump takes a DumpHandler and a list of DumpAdapters, and
// returns a new DumpHandler that applies the adapters in order.
func AdaptedDump(h DumpHandler, adapters ...DumpAdapter) DumpHandler {
	for i := len(adapters) - 1; i >= 0; i-- {
		h = adapters[i](h)
	}
	return h
}

// AdaptedRestore takes a RestoreHandler and a list of RestoreAdapters, and
// returns a new RestoreHandler that applies the adapters in order.
func AdaptedRestore(h RestoreHandler, adapters ...RestoreAdapter) RestoreHandler {
	for i := len(adapters) - 1; i >= 0; i-- {
		h = adapters[i](h)
	}
	return h
}
