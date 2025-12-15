package job

import (
	"context"
	"fmt"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/types"
	"github.com/rb-go/namegen"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const DEFAULT_LOG_PATH_FORMATTER string = "/var/log/cedana-output-%s.log"

// Adapter that manages the job state.
// Also attaches GPU support to the job, if requested.
// Allows management of existing processes as well (not started by the daemon).
func Manage(jobs Manager) types.Adapter[types.Run] {
	return func(next types.Run) types.Run {
		return func(ctx context.Context, opts types.Opts, resp *daemon.RunResp, req *daemon.RunReq) (code func() <-chan int, err error) {
			if req.JID == "" {
				req.JID = namegen.GetName(1)
			}

			job, err := jobs.New(req.JID, req.Type)
			if err != nil {
				return nil, status.Errorf(codes.Internal, "failed to create new job: %v", err)
			}

			if !req.Attachable {
				if req.Log == "" {
					req.Log = fmt.Sprintf(DEFAULT_LOG_PATH_FORMATTER, job.JID)
				}
			}

			job.SetLog(req.Log)
			job.SetDetails(req.Details)

			// Create child lifetime context, so we have cancellation ability over started process
			lifetime, cancel := context.WithCancel(opts.Lifetime)
			opts.Lifetime = lifetime

			code, err = next(ctx, opts, resp, req)
			if err != nil {
				jobs.Delete(job.JID)
				return nil, err
			}

			job.SetDetails(req.Details) // Set again, in case they got modified

			err = jobs.Manage(opts.Lifetime, job.JID, resp.PID, code())
			if err != nil {
				if req.Action == daemon.RunAction_START_NEW { // we don't want to cancel if manage was called for an existing process
					cancel()
				}
				jobs.Delete(job.JID)
				return nil, status.Errorf(codes.Internal, "failed to manage job: %v", err)
			}

			return code, nil
		}
	}
}
