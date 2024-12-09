package job

import (
	"context"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	criu_proto "buf.build/gen/go/cedana/criu/protocolbuffers/go/criu"
	"github.com/cedana/cedana/pkg/criu"
	"github.com/cedana/cedana/pkg/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// Adapter that fills in restore request details based on saved job info
// Post-restore, updates the saved job details.
func ManageRestore(jobs Manager) types.Adapter[types.Restore] {
	return func(next types.Restore) types.Restore {
		return func(ctx context.Context, server types.ServerOpts, nfy *criu.NotifyCallbackMulti, resp *daemon.RestoreResp, req *daemon.RestoreReq) (chan int, error) {
			jid := req.GetDetails().GetJID()

			if jid == "" {
				return nil, status.Errorf(codes.InvalidArgument, "missing JID in managed restore")
			}

			job := jobs.Get(jid)
			if job == nil {
				return nil, status.Errorf(codes.NotFound, "job %s not found", jid)
			}

			if job.IsRunning() {
				return nil, status.Errorf(codes.FailedPrecondition, "job %s is already running", jid)
			}

			// Fill in restore request details based on saved job info
			// TODO YA: Allow overriding job details, otherwise use saved job details
			req.Type = job.GetType()
			if req.Log == "" {
				req.Log = job.GetLog() // Use the same log file as it was before dump
			}
			req.Details = job.GetDetails()
			if req.Path == "" {
				req.Path = job.GetCheckpointPath()
			}
			if req.Path == "" {
				return nil, status.Errorf(codes.FailedPrecondition, "job % has no saved checkpoint. pass in path to override", jid)
			}
			if req.Criu == nil {
				req.Criu = &criu_proto.CriuOpts{}
			}
			req.Criu.RstSibling = proto.Bool(true) // Since managed, we restore as child

			// Create child lifetime context, so we have cancellation ability over restored
			// process created by the next handler(s).
			lifetime, cancel := context.WithCancel(server.Lifetime)
			server.Lifetime = lifetime

			// Import saved notify callbacks
			nfy.ImportCallback(job.CRIUCallback)

			exited, err := next(ctx, server, nfy, resp, req)
			if err != nil {
				cancel()
				return nil, err
			}

			job.SetLog(req.Log)
			job.SetProcess(resp.GetState())

			err = jobs.Manage(ctx, jid, resp.PID, exited)
			if err != nil {
				cancel()
				return nil, status.Errorf(codes.Internal, "failed to manage restored job: %v", err)
			}

			return exited, nil
		}
	}
}
