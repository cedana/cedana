package adapters

import (
	"context"
	"fmt"

	"github.com/cedana/cedana/pkg/api/daemon"
	"github.com/cedana/cedana/pkg/types"
)

// Defines all the adapters that manage the job-level details
// Job is a high-level concept that can be used to represent a managed process, or container.

// Adapter that fills in request details based on saved job info
func FillRequestIfJob(h types.DumpHandler) types.DumpHandler {
	return func(ctx context.Context, resp *daemon.DumpResp, req *daemon.DumpReq) error {
		if req.GetDetails().GetType() == "job" {
			// Get job info from the request
			return fmt.Errorf("not implemented")
		}
		return h(ctx, resp, req)
	}
}
