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
		return func(ctx context.Context, opts types.Opts, resp *daemon.DumpResp, req *daemon.DumpReq) (code func() <-chan int, err error) {
			jid := req.GetDetails().GetJID()

			if jid == "" {
				return nil, status.Errorf(codes.InvalidArgument, "missing JID for managed dump")
			}

			job := jobs.Get(jid)
			if job == nil {
				return nil, status.Errorf(codes.NotFound, "job %s not found", jid)
			}

			if !job.IsRunning() {
				return nil, status.Errorf(codes.FailedPrecondition, "job %s is not running (status: %s)", jid, job.Status())
			}

			// Fill in dump request details based on saved job info

			req.Type = job.GetType()
			resp.State = job.GetState()
			if req.Name == "" {
				req.Name = fmt.Sprintf("dump-%s-%s-%d", req.Type, jid, time.Now().Unix())
			}

			// Use saved job details, but allow overriding from request
			mergedDetails := job.GetDetails()
			proto.Merge(mergedDetails, req.GetDetails())
			req.Details = mergedDetails

			code, err = next(ctx, opts, resp, req)
			if err != nil {
				return code, err
			}

			job.Sync(resp.GetState())

			jobs.AddCheckpoint(jid, resp.GetPaths())

			return code, nil
		}
	}
}
