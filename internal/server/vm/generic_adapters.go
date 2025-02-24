package vm

import (
	"context"

	"github.com/cedana/cedana/pkg/plugins"
	"github.com/cedana/cedana/pkg/types"
)

// Dummy function that just calls next for VM Dump
func New[REQ, RESP any](manager plugins.Manager) types.Adapter[types.Handler[REQ, RESP]] {
	return func(next types.Handler[REQ, RESP]) types.Handler[REQ, RESP] {
		return func(ctx context.Context, opts types.Opts, resp *RESP, req *REQ) (chan int, error) {
			return next(ctx, opts, resp, req)
		}
	}
}
