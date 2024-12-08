package adapters

// Defines all the adapters that manage the job-level details.
// Job is a high-level concept that can be used to represent a managed process, or container.

import (
	"context"
	"fmt"
	"os"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	criu_proto "buf.build/gen/go/cedana/criu/protocolbuffers/go/criu"
	"github.com/cedana/cedana/internal/server/job"
	"github.com/cedana/cedana/pkg/criu"
	"github.com/cedana/cedana/pkg/keys"
	"github.com/cedana/cedana/pkg/types"
	"github.com/rb-go/namegen"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

const (
	DEFAULT_LOG_PATH_FORMATTER string      = "/var/log/cedana-output-%s.log"
	LOG_FILE_PERMS             os.FileMode = 0o644
	LOG_FILE_FLAGS             int         = os.O_CREATE | os.O_WRONLY | os.O_APPEND | os.O_TRUNC
)

//////////////////////
//// Run Adapters ////
//////////////////////

// Adapter that manages the job state.
// Also attaches GPU support to the job, if requested.
func Manage(jobs job.Manager) types.Adapter[types.Run] {
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
				ctx = context.WithValue(ctx, keys.RUN_LOG_FILE_CONTEXT_KEY, logFile)
			}

			job.SetLog(req.Log)
			job.SetDetails(req.Details)

			if req.GPUEnabled {
				next = next.With(GPUSupport(jobs))
			}

			// Create child lifetime context, so we have cancellation ability over started process
			lifetime, cancel := context.WithCancel(server.Lifetime)
			server.Lifetime = lifetime

			exited, err := next(ctx, server, resp, req)
			if err != nil {
				jobs.Delete(job.JID)
				return nil, err
			}

			err = jobs.Manage(ctx, job.JID, resp.PID, exited)
			if err != nil {
				cancel()
				jobs.Delete(job.JID)
				return nil, status.Errorf(codes.Internal, "failed to manage job: %v", err)
			}

			return exited, nil
		}
	}
}

////////////////////////
//// Dump Adapters /////
////////////////////////

// Adapter that fills in dump request details based on saved job info.
// Post-dump, updates the saved job details.
func ManageDump(jobs job.Manager) types.Adapter[types.Dump] {
	return func(next types.Dump) types.Dump {
		return func(ctx context.Context, server types.ServerOpts, nfy *criu.NotifyCallbackMulti, resp *daemon.DumpResp, req *daemon.DumpReq) (chan int, error) {
			jid := req.GetDetails().GetJID()

			if jid == "" {
				return next(ctx, server, nfy, resp, req)
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

			job.SetCheckpointPath(resp.GetPath())
			job.SetProcess(resp.GetState())

			return exited, nil
		}
	}
}

//////////////////////////
//// Restore Adapters ////
//////////////////////////

// Adapter that fills in restore request details based on saved job info
// Post-restore, updates the saved job details.
func ManageRestore(jobs job.Manager) types.Adapter[types.Restore] {
	return func(next types.Restore) types.Restore {
		return func(ctx context.Context, server types.ServerOpts, nfy *criu.NotifyCallbackMulti, resp *daemon.RestoreResp, req *daemon.RestoreReq) (chan int, error) {
			jid := req.GetDetails().GetJID()

			if jid == "" { // not a managed restore
				return next(ctx, server, nfy, resp, req)
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
