package adapters

import (
	"context"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/plugins/runc"
	"github.com/cedana/cedana/pkg/criu"
	"github.com/cedana/cedana/pkg/types"
)

// This file contains all the adapters that fill in missing request details
// with defaults

const defaultRoot = "/run/runc"

///////////////////////
//// Dump Adapters ////
///////////////////////

func FillMissingDumpDefaults(next types.Dump) types.Dump {
	return func(ctx context.Context, server types.ServerOpts, nfy *criu.NotifyCallbackMulti, resp *daemon.DumpResp, req *daemon.DumpReq) (chan int, error) {
		if req.GetDetails() == nil {
			req.Details = &daemon.Details{}
		}

		if req.GetDetails().GetRunc() == nil {
			req.Details.Runc = &runc.Details{}
		}

		if req.GetDetails().GetRunc().GetRoot() == "" {
			req.Details.Runc.Root = defaultRoot
		}

		return next(ctx, server, nfy, resp, req)
	}
}

//////////////////////////
//// Restore Adapters ////
//////////////////////////
