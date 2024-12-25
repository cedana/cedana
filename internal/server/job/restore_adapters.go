package job

import (
	"context"
	"fmt"
	"os"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	criu_proto "buf.build/gen/go/cedana/criu/protocolbuffers/go/criu"
	"github.com/cedana/cedana/pkg/keys"
	"github.com/cedana/cedana/pkg/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// Adapter that fills in restore request details based on saved job info
// Post-restore, updates the saved job details.
func ManageRestore(jobs Manager) types.Adapter[types.Restore] {
	return func(next types.Restore) types.Restore {
		return func(ctx context.Context, server types.ServerOpts, resp *daemon.RestoreResp, req *daemon.RestoreReq) (chan int, error) {
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

			req.Type = job.GetType()

			if !req.Attachable {
				if req.Log == "" {
					req.Log = job.GetLog() // Use the same log file as it was before dump
				}
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

			// Use saved job details, but allow overriding from request
			proto.Merge(req.Details, job.GetDetails())

			if req.Path == "" {
				req.Path = jobs.GetLatestCheckpoint(jid).GetPath()
			}
			if req.Path == "" {
				return nil, status.Errorf(codes.FailedPrecondition, "job %s has no saved checkpoint. pass in path to override", jid)
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
			server.CRIUCallback.IncludeMulti(jobs.CRIUCallback(server.Lifetime, jid))

			exited, err := next(ctx, server, resp, req)
			if err != nil {
				cancel()
				return nil, err
			}

			job.SetLog(req.Log)

			err = jobs.Manage(server.Lifetime, jid, resp.PID, exited)
			if err != nil {
				cancel()
				return nil, status.Errorf(codes.Internal, "failed to manage restored job: %v", err)
			}

			return exited, nil
		}
	}
}
