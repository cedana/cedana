package job

import (
	"context"
	"fmt"
	"os"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/internal/server/gpu"
	"github.com/cedana/cedana/pkg/keys"
	"github.com/cedana/cedana/pkg/types"
	"github.com/rb-go/namegen"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	DEFAULT_LOG_PATH_FORMATTER string      = "/var/log/cedana-output-%s.log"
	LOG_FILE_PERMS             os.FileMode = 0o644
	LOG_FILE_FLAGS             int         = os.O_CREATE | os.O_WRONLY | os.O_APPEND | os.O_TRUNC
)

// Adapter that manages the job state.
// Also attaches GPU support to the job, if requested.
// Allows management of existing processes as well (not started by the daemon).
func Manage(jobs Manager) types.Adapter[types.Run] {
	return func(next types.Run) types.Run {
		return func(ctx context.Context, server types.ServerOpts, resp *daemon.RunResp, req *daemon.RunReq) (chan int, error) {
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
				logFile, err := os.OpenFile(req.Log, LOG_FILE_FLAGS, LOG_FILE_PERMS)
				if err != nil {
					return nil, status.Errorf(codes.Internal, "failed to open log file: %v", err)
				}
				defer logFile.Close()
				ctx = context.WithValue(ctx, keys.LOG_FILE_CONTEXT_KEY, logFile)
			}

			job.SetLog(req.Log)
			job.SetDetails(req.Details)

			if req.GPUEnabled {
				next = next.With(gpu.Attach(jobs.GPUs()))
			}

			// Create child lifetime context, so we have cancellation ability over started process
			lifetime, cancel := context.WithCancel(server.Lifetime)
			server.Lifetime = lifetime

			exited, err := next(ctx, server, resp, req)
			if err != nil {
				jobs.Delete(job.JID)
				return nil, err
			}

			err = jobs.Manage(server.Lifetime, job.JID, resp.PID, exited)
			if err != nil {
				if req.Action == daemon.RunAction_START_NEW { // we don't want to cancel if manage was called for an existing process
					cancel()
				}
				jobs.Delete(job.JID)
				return nil, status.Errorf(codes.Internal, "failed to manage job: %v", err)
			}

			return exited, nil
		}
	}
}
