package types

// Defines the types and functions used to create and manage server handlers, adapters, and middleware.

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
		WG           *sync.WaitGroup
		CRIU         *criu.Criu
		CRIUCallback *criu.NotifyCallbackMulti
		Plugins      plugins.Manager
		Lifetime     context.Context
	}

	Dump    = Handler[daemon.DumpReq, daemon.DumpResp]
	Restore = Handler[daemon.RestoreReq, daemon.RestoreResp]
	Run     = Handler[daemon.RunReq, daemon.RunResp]

	Handler[REQ, RESP any] func(context.Context, ServerOpts, *RESP, *REQ) (exited chan int, err error)
)
