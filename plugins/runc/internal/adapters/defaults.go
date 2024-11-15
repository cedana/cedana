package adapters

import (
	"context"

	"github.com/cedana/cedana/pkg/api/daemon"
	"github.com/cedana/cedana/pkg/api/plugins/runc"
	"github.com/cedana/cedana/pkg/types"
)

// This file contains all the adapters that fill in missing request details
// with defaults

const defaultRoot = "/run/runc"

///////////////////////
//// Dump Adapters ////
///////////////////////

func FillMissingDumpDefaults(next types.Dump) types.Dump {
	return func(ctx context.Context, server types.ServerOpts, resp *daemon.DumpResp, req *daemon.DumpReq) error {
		if req.GetDetails() == nil {
			req.Details = &daemon.Details{}
		}

		if req.GetDetails().GetRunc() == nil {
			req.Details.Runc = &runc.Details{}
		}

		if req.GetDetails().GetRunc().GetRoot() == "" {
			req.Details.Runc.Root = defaultRoot
		}

		return next(ctx, server, resp, req)
	}
}

//////////////////////////
//// Restore Adapters ////
//////////////////////////
