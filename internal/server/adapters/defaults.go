package adapters

import (
	"context"
	"sync"

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
func FillMissingDumpDefaults(next types.DumpHandler) types.DumpHandler {
	return func(ctx context.Context, wg *sync.WaitGroup, resp *daemon.DumpResp, req *daemon.DumpReq) error {
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

		return next(ctx, wg, resp, req)
	}
}

//////////////////////////
//// Restore Adapters ////
//////////////////////////

// Adapter that fills missing info from the request using config defaults
func FillMissingRestoreDefaults(next types.RestoreHandler) types.RestoreHandler {
	return func(ctx context.Context, lifetimeCtx context.Context, wg *sync.WaitGroup, resp *daemon.RestoreResp, req *daemon.RestoreReq) (chan int, error) {
		// Nothing to do, yet

		return next(ctx, lifetimeCtx, wg, resp, req)
	}
}

////////////////////////
//// Start Adapters ////
////////////////////////

// Adapter that fills missing info from the request using config defaults
func FillMissingStartDefaults(next types.StartHandler) types.StartHandler {
	return func(ctx context.Context, lifetimeCtx context.Context, wg *sync.WaitGroup, resp *daemon.StartResp, req *daemon.StartReq) (chan int, error) {
		// Nothing to fill in for now
		return next(ctx, lifetimeCtx, wg, resp, req)
	}
}
