package adapters

// Defines all the adapters that manage the job-level details
// Job is a high-level concept that can be used to represent a managed process, or container.

import (
	"context"
	"fmt"

	"github.com/cedana/cedana/pkg/api/daemon"
	"github.com/cedana/cedana/pkg/types"
)

////////////////////////
//// Dump Adapters /////
////////////////////////

// Adapter that fills in dump request details based on saved job info.
// Post-dump, updates the saved job details.
func JobDumpAdapter(h types.DumpHandler) types.DumpHandler {
	return func(ctx context.Context, resp *daemon.DumpResp, req *daemon.DumpReq) error {
		if req.GetType() == "job" {
			// Get job info from the request
			return fmt.Errorf("not implemented")
		}
		return h(ctx, resp, req)
	}
}

//////////////////////////
//// Restore Adapters ////
//////////////////////////

// Adapter that fills in restore request details based on saved job info
// Post-restore, updates the saved job details.
func JobRestoreAdapter(h types.RestoreHandler) types.RestoreHandler {
	return func(ctx context.Context, resp *daemon.RestoreResp, req *daemon.RestoreReq) error {
		if req.GetType() == "job" {
			// Get job info from the request
			return fmt.Errorf("not implemented")
		}
		return h(ctx, resp, req)
	}
}
