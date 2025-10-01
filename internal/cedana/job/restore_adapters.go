package job

import (
	"context"
	"fmt"
	"os"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	criu_proto "buf.build/gen/go/cedana/criu/protocolbuffers/go/criu"
	"github.com/cedana/cedana/pkg/keys"
	"github.com/cedana/cedana/pkg/types"
	"github.com/rb-go/namegen"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// Adapter that fills in restore request details based on saved job info
// Post-restore, updates the saved job details.
func ManageRestore(jobs Manager) types.Adapter[types.Restore] {
	return func(next types.Restore) types.Restore {
		return func(ctx context.Context, opts types.Opts, resp *daemon.RestoreResp, req *daemon.RestoreReq) (code func() <-chan int, err error) {
			jid := req.GetDetails().GetJID()

			var job *Job

			// If attachable then we create a new job, if no JID is provided.
			// Otherwise we use the existing job with the provided JID.

			if jid == "" {
				if req.Attachable {
					jid = namegen.GetName(1)
					req.Details.JID = proto.String(jid)

					job, err = jobs.New(jid, req.Type)
					if err != nil {
						return nil, status.Errorf(codes.Internal, "failed to create new job: %v", err)
					}
				} else {
					return nil, status.Errorf(codes.InvalidArgument, "missing JID in managed restore")
				}
			}

			job = jobs.Get(jid)
			if job == nil {
				return nil, status.Errorf(codes.NotFound, "job %s not found", jid)
			}

			if job.IsRunning() {
				return nil, status.Errorf(codes.FailedPrecondition, "job %s is already running", jid)
			}

			// Fill in restore request details based on saved job info

			req.Type = job.GetType()

			if !req.Attachable {
				if req.Log == "" {
					req.Log = job.GetLog() // Use the same log file as it was before dump
				}
				if req.Log == "" {
					req.Log = fmt.Sprintf(DEFAULT_LOG_PATH_FORMATTER, job.JID)
				}
				outFile, err := os.OpenFile(req.Log, OUT_FILE_FLAGS, OUT_FILE_PERMS)
				if err != nil {
					return nil, status.Errorf(codes.Internal, "failed to open log file: %v", err)
				}
				defer outFile.Close()
				err = os.Chown(req.Log, int(req.UID), int(req.GID))
				if err != nil {
					return nil, status.Errorf(codes.Internal, "failed to change log file owner: %v", err)
				}
				ctx = context.WithValue(ctx, keys.OUT_FILE_CONTEXT_KEY, outFile)
			}

			// Use saved job details, but allow overriding from request

			if job.GetDetails() != nil {
				mergedDetails := proto.Clone(job.GetDetails()).(*daemon.Details)
				proto.Merge(mergedDetails, req.GetDetails())
				req.Details = mergedDetails
			}

			if req.Path == "" {
				req.Path = jobs.GetLatestCheckpoint(jid).GetPath()
			}
			if req.Path == "" {
				return nil, status.Errorf(codes.FailedPrecondition, "job %s has no saved checkpoint. pass in path to override", jid)
			}
			if req.Criu == nil {
				req.Criu = &criu_proto.CriuOpts{}
			}
			if req.Env == nil {
				req.Env = []string{}
			}

			// Create child lifetime context, so we have cancellation ability over restored
			// process created by the next handler(s).

			lifetime, cancel := context.WithCancel(opts.Lifetime)
			opts.Lifetime = lifetime

			code, err = next(ctx, opts, resp, req)
			if err != nil {
				cancel()
				return nil, err
			}

			job.SetDetails(req.Details) // Set again, in case they got modified
			job.SetLog(req.Log)

			err = jobs.Manage(opts.Lifetime, jid, resp.PID, code())
			if err != nil {
				cancel()
				return nil, status.Errorf(codes.Internal, "failed to manage restored job: %v", err)
			}

			return code, nil
		}
	}
}
