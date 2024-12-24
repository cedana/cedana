package job

import (
	"context"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// Adapter that fills in dump request details based on saved job info.
// Post-dump, updates the saved job details.
func ManageDump(jobs Manager) types.Adapter[types.Dump] {
	return func(next types.Dump) types.Dump {
		return func(ctx context.Context, server types.ServerOpts, resp *daemon.DumpResp, req *daemon.DumpReq) (chan int, error) {
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

			req.Type = job.GetType()
			resp.State = job.GetState()

			// Use saved job details, but allow overriding from request
			proto.Merge(req.Details, job.GetDetails())

			// Import saved notify callbacks
			server.CRIUCallback.IncludeMulti(jobs.CRIUCallback(server.Lifetime, jid))

			exited, err := next(ctx, server, resp, req)
			if err != nil {
				return exited, err
			}

			job.SetState(resp.GetState())

			jobs.AddCheckpoint(jid, resp.GetPath())

			return exited, nil
		}
	}
}
