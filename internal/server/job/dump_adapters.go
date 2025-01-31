package job

import (
	"context"
	"fmt"
	"time"

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
		return func(ctx context.Context, opts types.Opts, resp *daemon.DumpResp, req *daemon.DumpReq) (chan int, error) {
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
			if req.Name == "" {
				req.Name = fmt.Sprintf("dump-%s-%s-%d", req.Type, jid, time.Now().Unix())
			}

			// Use saved job details, but allow overriding from request
			mergedDetails := proto.Clone(job.GetDetails()).(*daemon.Details)
			proto.Merge(mergedDetails, req.GetDetails())
			req.Details = mergedDetails

			// Import saved notify callbacks
			opts.CRIUCallback.IncludeMulti(jobs.CRIUCallback(opts.Lifetime, jid, req.Stream))

			exited, err := next(ctx, opts, resp, req)
			if err != nil {
				return exited, err
			}

			job.SetState(resp.GetState())

			jobs.AddCheckpoint(jid, resp.GetPath())

			return exited, nil
		}
	}
}
