package adapters

import (
	"context"

	"github.com/cedana/cedana/internal/config"
	"github.com/cedana/cedana/pkg/api/criu"
	"github.com/cedana/cedana/pkg/api/daemon"
	"github.com/cedana/cedana/pkg/types"
	"google.golang.org/protobuf/proto"
)

// This file contains all the adapters that fill in missing request details
// with defaults

///////////////////////
//// Dump Adapters ////
///////////////////////

// Adapter that fills missing info from the request using config defaults
func FillMissingDumpDefaults(next types.Handler[types.Dump]) types.Handler[types.Dump] {
	next.Handle = func(ctx context.Context, resp *daemon.DumpResp, req *daemon.DumpReq) error {
		if req.GetDir() == "" {
			req.Dir = config.Get(config.STORAGE_DUMP_DIR)
		}

		if req.GetCriu() == nil {
			req.Criu = &criu.CriuOpts{}
		}

		// Only override if unset
		if req.GetCriu().LeaveRunning == nil {
			req.Criu.LeaveRunning = proto.Bool(config.Get(config.CRIU_LEAVE_RUNNING))
		}

		return next.Handle(ctx, resp, req)
	}
	return next
}

//////////////////////////
//// Restore Adapters ////
//////////////////////////

// Adapter that fills missing info from the request using config defaults
func FillMissingRestoreDefaults(next types.Handler[types.Restore]) types.Handler[types.Restore] {
	next.Handle = func(ctx context.Context, resp *daemon.RestoreResp, req *daemon.RestoreReq) (chan int, error) {
		// Nothing to do, yet

		return next.Handle(ctx, resp, req)
	}
	return next
}

////////////////////////
//// Start Adapters ////
////////////////////////

// Adapter that fills missing info from the request using config defaults
func FillMissingStartDefaults(next types.Handler[types.Start]) types.Handler[types.Start] {
	next.Handle = func(ctx context.Context, resp *daemon.StartResp, req *daemon.StartReq) (chan int, error) {
		// Nothing to fill in for now
		return next.Handle(ctx, resp, req)
	}
	return next
}
