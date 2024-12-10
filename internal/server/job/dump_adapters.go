package job

import (
	"context"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/criu"
	"github.com/cedana/cedana/pkg/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Adapter that fills in dump request details based on saved job info.
// Post-dump, updates the saved job details.
func ManageDump(jobs Manager) types.Adapter[types.Dump] {
	return func(next types.Dump) types.Dump {
		return func(ctx context.Context, server types.ServerOpts, nfy *criu.NotifyCallbackMulti, resp *daemon.DumpResp, req *daemon.DumpReq) (chan int, error) {
			jid := req.GetDetails().GetJID()

			if jid == "" {
				return nil, status.Errorf(codes.InvalidArgument, "missing JID for managed dump")
			}

			job := jobs.Get(jid)
			if job == nil {
				return nil, status.Errorf(codes.NotFound, "job %s not found", jid)
			}

			if !job.IsRunning() {
				return nil, status.Errorf(codes.FailedPrecondition, "job %s is not running", jid)
			}

			// Fill in dump request details based on saved job info
			// TODO YA: Allow overriding job details, otherwise use saved job details
			req.Details = job.GetDetails()
			req.Type = job.GetType()
			resp.State = job.GetProcess()

			// Import saved notify callbacks
			nfy.ImportCallback(job.CRIUCallback)

			exited, err := next(ctx, server, nfy, resp, req)
			if err != nil {
				return exited, err
			}

			job.AddCheckpoint(resp.GetPath())
			job.SetProcess(resp.GetState())

			return exited, nil
		}
	}
}
