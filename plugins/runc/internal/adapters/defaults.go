package adapters

import (
	"context"
	"sync"

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

func FillMissingDumpDefaults(h types.DumpHandler) types.DumpHandler {
	return func(ctx context.Context, wg *sync.WaitGroup, resp *daemon.DumpResp, req *daemon.DumpReq) error {
		if req.GetDetails() == nil {
			req.Details = &daemon.Details{}
		}

		if req.GetDetails().GetRunc() == nil {
			req.Details.Runc = &runc.Details{}
		}

		if req.GetDetails().GetRunc().GetRoot() == "" {
			req.Details.Runc.Root = defaultRoot
		}

		return h(ctx, wg, resp, req)
	}
}

//////////////////////////
//// Restore Adapters ////
//////////////////////////
