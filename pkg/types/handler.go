package types

// Defines the types and functions used to create and manage server handlers, adapters, and middleware.

import (
	"context"
	"sync"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/criu"
	"github.com/cedana/cedana/pkg/io"
	"github.com/cedana/cedana/pkg/plugins"
	"github.com/spf13/afero"
)

type (
	// Opts is intended to be passed by **value** to each handler, so that each handler can modify it
	// before passing it to the next handler in the chain, without affecting the original value.
	Opts struct {
		WG           *sync.WaitGroup
		CRIU         *criu.Criu
		CRIUCallback *criu.NotifyCallbackMulti
		Plugins      plugins.Manager
		Lifetime     context.Context
		Storage      io.Storage
		DumpFs       afero.Fs
		FdStore      *sync.Map
	}

	Dump      = Handler[daemon.DumpReq, daemon.DumpResp]
	Restore   = Handler[daemon.RestoreReq, daemon.RestoreResp]
	Freeze    = Handler[daemon.DumpReq, daemon.DumpResp]
	Unfreeze  = Handler[daemon.DumpReq, daemon.DumpResp]
	Run       = Handler[daemon.RunReq, daemon.RunResp]
	DumpVM    = Handler[daemon.DumpVMReq, daemon.DumpVMResp]
	RestoreVM = Handler[daemon.RestoreVMReq, daemon.RestoreVMResp]
	// RunVM     = Handler[daemon.RunVMReq, daemon.RunVMResp]

	Handler[REQ, RESP any] func(context.Context, Opts, *RESP, *REQ) (code func() <-chan int, err error)
)
