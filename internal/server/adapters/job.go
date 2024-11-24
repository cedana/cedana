package adapters

// Defines all the adapters that manage the job-level details.
// Job is a high-level concept that can be used to represent a managed process, or container.

import (
	"context"
	"fmt"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"buf.build/gen/go/cedana/criu/protocolbuffers/go/criu"
	"github.com/cedana/cedana/internal/server/job"
	"github.com/cedana/cedana/pkg/types"
	"github.com/rb-go/namegen"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

const DEFAULT_LOG_PATH_FORMATTER string = "/var/log/cedana-output-%s.log"

////////////////////////
//// Start Adapters ////
////////////////////////

// Adapter that manages the job state.
// Also attaches GPU support to the job, if requested.
func Manage(jobs job.Manager) types.Adapter[types.Start] {
	return func(next types.Start) types.Start {
		return func(ctx context.Context, server types.ServerOpts, resp *daemon.StartResp, req *daemon.StartReq) (chan int, error) {
			if req.JID == "" {
				req.JID = namegen.GetName(1)
			}

			job, err := jobs.New(req.JID, req.Type)
			if err != nil {
				return nil, status.Errorf(codes.Internal, "failed to create new job: %v", err)
			}

			if req.Log == "" {
				req.Log = fmt.Sprintf(DEFAULT_LOG_PATH_FORMATTER, job.JID)
			}
			job.SetLog(req.Log)

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

			err = jobs.Manage(ctx, server.WG, job.JID, resp.PID, exited)
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
		return func(ctx context.Context, server types.ServerOpts, resp *daemon.DumpResp, req *daemon.DumpReq) (exited chan int, err error) {
			jid := req.GetDetails().GetJID()

			if jid == "" {
				return next(ctx, server, resp, req)
			}

			job := jobs.Get(jid)
			if job == nil {
				return nil, status.Errorf(codes.NotFound, "job not found")
			}

			// Fill in dump request details based on saved job info
			// TODO YA: Allow overriding job details, otherwise use saved job details
			req.Details = job.GetDetails()
			req.Type = job.GetType()

			exited, err = next(ctx, server, resp, req)
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
		return func(ctx context.Context, server types.ServerOpts, resp *daemon.RestoreResp, req *daemon.RestoreReq) (chan int, error) {
			jid := req.GetDetails().GetJID()

			if jid == "" {
				return next(ctx, server, resp, req)
			}

			job := jobs.Get(jid)
			if job == nil {
				return nil, status.Errorf(codes.NotFound, "job not found")
			}

			// Fill in restore request details based on saved job info
			// TODO YA: Allow overriding job details, otherwise use saved job details
			if req.Log == "" {
				req.Log = job.GetLog() // Use the same log file as it was before dump
			}
			req.Details = job.GetDetails()
			if req.Path == "" {
				req.Path = job.GetCheckpointPath()
			}
			if req.Criu == nil {
				req.Criu = &criu.CriuOpts{}
			}
			req.Type = job.GetType()
			req.Criu.RstSibling = proto.Bool(true) // Since managed, we restore as child

			// Create child lifetime context, so we have cancellation ability over restored
			// process created by the next handler(s).
			lifetime, cancel := context.WithCancel(server.Lifetime)
			server.Lifetime = lifetime

			exited, err := next(ctx, server, resp, req)
			if err != nil {
				cancel()
				return nil, err
			}

			job.SetLog(req.Log)
			job.SetProcess(resp.GetState())

			err = jobs.Manage(ctx, server.WG, jid, resp.PID, exited)
			if err != nil {
				cancel()
				return nil, status.Errorf(codes.Internal, "failed to manage restored job: %v", err)
			}

			return exited, nil
		}
	}
}
