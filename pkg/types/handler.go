package types

// Defines the types and functions used to create and manage server handlers, adapters, and middleware.

import (
	"context"
	"io"
	"os"
	"sync"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/criu"
	cedana_io "github.com/cedana/cedana/pkg/io"
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
		Storage      cedana_io.Storage // Direct R/W access to underlying storage of the dump (use DumpFs instead)
		DumpFs       afero.Fs          // Full filesystem to use for any dump/restore operations
		HostFs       afero.Fs          // Filesystem for host operations (defaults to OS filesystem if nil)
		FdStore      *sync.Map
		Serverless   bool // Whether the operation is being performed in serverless mode

		IO struct {
			Stdin  io.Reader
			Stdout io.Writer
			Stderr io.Writer
		}
		ExtraFiles   []*os.File
		InheritFdMap map[string]int32
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

func Details[REQ any](req *REQ) *daemon.Details {
	switch r := any(req).(type) {
	case *daemon.DumpReq:
		return r.GetDetails()
	case *daemon.RestoreReq:
		return r.GetDetails()
	case *daemon.RunReq:
		return r.GetDetails()
	case *daemon.DumpVMReq:
		return r.GetDetails()
	default:
		panic("unsupported type for Details extraction")
	}
}

func PID[RESP any](resp *RESP) uint32 {
	switch r := any(resp).(type) {
	case *daemon.RunResp:
		return r.PID
	case *daemon.RestoreResp:
		return r.PID
	default:
		panic("unsupported type for PID extraction")
	}
}

func SetPID[RESP any](resp *RESP, pid uint32) {
	switch r := any(resp).(type) {
	case *daemon.RunResp:
		r.PID = pid
	case *daemon.RestoreResp:
		r.PID = pid
	}
}

func Attachable[REQ any](req *REQ) bool {
	switch r := any(req).(type) {
	case *daemon.RunReq:
		return r.Attachable
	case *daemon.RestoreReq:
		return r.Attachable
	default:
		panic("unsupported type for Attachable extraction")
	}
}

func Log[REQ any](req *REQ) string {
	switch r := any(req).(type) {
	case *daemon.RunReq:
		return r.Log
	case *daemon.RestoreReq:
		return r.Log
	default:
		panic("unsupported type for Log extraction")
	}
}

func UID[REQ any](req *REQ) uint32 {
	switch r := any(req).(type) {
	case *daemon.RunReq:
		return r.UID
	case *daemon.RestoreReq:
		return r.UID
	default:
		panic("unsupported type for UID extraction")
	}
}

func GID[REQ any](req *REQ) uint32 {
	switch r := any(req).(type) {
	case *daemon.RunReq:
		return r.GID
	case *daemon.RestoreReq:
		return r.GID
	default:
		panic("unsupported type for GID extraction")
	}
}
