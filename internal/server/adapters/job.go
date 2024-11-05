package adapters

// Defines all the adapters that manage the job-level details
// Job is a high-level concept that can be used to represent a managed process, or container.

import (
	"context"
	"sync"

	"github.com/cedana/cedana/internal/db"
	"github.com/cedana/cedana/pkg/api/daemon"
	"github.com/cedana/cedana/pkg/types"
	"github.com/cedana/cedana/pkg/utils"
	"github.com/rb-go/namegen"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

////////////////////////
//// Dump Adapters /////
////////////////////////

// Adapter that fills in dump request details based on saved job info.
// Post-dump, updates the saved job details.
func JobDumpAdapter(db db.DB) types.Adapter[types.DumpHandler] {
	return func(h types.DumpHandler) types.DumpHandler {
		return func(ctx context.Context, wg *sync.WaitGroup, resp *daemon.DumpResp, req *daemon.DumpReq) error {
			if req.GetType() != "job" {
				return h(ctx, wg, resp, req)
			}

			// Fill in dump request details based on saved job info
			jid := req.GetDetails().GetJID()
			if jid == "" {
				return status.Errorf(codes.InvalidArgument, "JID is required")
			}

			job, err := db.GetJob(ctx, jid)
			if err != nil {
				return status.Errorf(codes.NotFound, "job not found: %v", err)
			}

			// TODO YA: Allow overriding job details, otherwise use saved job details
			req.Details = job.Details
			req.Type = job.Type

			err = h(ctx, wg, resp, req)
			if err != nil {
				return err
			}

			wg.Add(1)
			go func() {
				defer wg.Done()
				ctx := context.WithoutCancel(ctx)

				job.CheckpointPath = resp.GetPath()
				job.Process = resp.GetState()

				// Wait for process exit, if not using --leave-running
				// to avoid race with state update
				if !req.GetCriu().GetLeaveRunning() {
					utils.WaitForPid(job.Process.GetPID())
					if job.Process.Info == nil {
						job.Process.Info = &daemon.ProcessInfo{}
					}
					job.Process.Info.IsRunning = false
				}

				err = db.PutJob(ctx, jid, job)
				if err != nil {
					log.Warn().Err(err).Str("JID", jid).Msg("failed to update job after dump")
				}
			}()

			return nil
		}
	}
}

//////////////////////////
//// Restore Adapters ////
//////////////////////////

// Adapter that fills in restore request details based on saved job info
// Post-restore, updates the saved job details.
func JobRestoreAdapter(db db.DB) types.Adapter[types.RestoreHandler] {
	return func(h types.RestoreHandler) types.RestoreHandler {
		return func(ctx context.Context, lifetimeCtx context.Context, wg *sync.WaitGroup, resp *daemon.RestoreResp, req *daemon.RestoreReq) (chan int, error) {
			if req.GetType() != "job" {
				return h(ctx, lifetimeCtx, wg, resp, req)
			}

			// Fill in restore request details based on saved job info
			jid := req.GetDetails().GetJID()
			if jid == "" {
				return nil, status.Errorf(codes.InvalidArgument, "JID is required")
			}

			job, err := db.GetJob(ctx, jid)
			if err != nil {
				return nil, status.Errorf(codes.NotFound, "job not found: %v", err)
			}

			// TODO YA: Allow overriding job details, otherwise use saved job details
			req.Details = job.Details
			if req.Path == "" {
				req.Path = job.CheckpointPath
			}
			if req.Criu == nil {
				req.Criu = &daemon.CriuOpts{}
			}
			req.Criu.RstSibling = true
			req.Type = job.Type

			lifetimeCtx, cancel := context.WithCancel(lifetimeCtx)
			exited, err := h(ctx, lifetimeCtx, wg, resp, req)
			if err != nil {
				cancel()
				return nil, err
			}

			// Get process state if only if possible, as the process
			// may have already exited by the time we get here.
			state := &daemon.ProcessState{}
			err = utils.FillProcessState(ctx, resp.PID, state)
			if err == nil {
				job.Process = state

				// Save job details
				err = db.PutJob(ctx, jid, job)
				if err != nil {
					cancel()
					return nil, status.Errorf(codes.Internal, "failed to save job details: %v", err)
				}

			}

			// Wait for process exit to update job
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-exited
				log.Info().Str("JID", jid).Uint32("PID", resp.PID).Msg("job exited")
				job.Process.Info = &daemon.ProcessInfo{IsRunning: false}
				ctx := context.WithoutCancel(ctx)
				err := db.PutJob(ctx, jid, job)
				if err != nil {
					log.Warn().Err(err).Str("JID", jid).Msg("failed to update job after exit")
				}
			}()

			return exited, nil
		}
	}
}

////////////////////////
//// Start Adapters ////
////////////////////////

// Adapter that manages the job state.
// Also attaches GPU support to the job, if requested.
func JobStartAdapter(db db.DB) types.Adapter[types.StartHandler] {
	return func(h types.StartHandler) types.StartHandler {
		return func(ctx context.Context, lifetimeCtx context.Context, wg *sync.WaitGroup, resp *daemon.StartResp, req *daemon.StartReq) (chan int, error) {
			jid := req.GetJID()
			if jid == "" {
				jid = namegen.GetName(1)
			}
			resp.JID = jid

			// Check if the job already exists
			_, err := db.GetJob(ctx, jid)
			if err == nil {
				return nil, status.Errorf(codes.AlreadyExists, "job already exists")
			}

			if req.GetGPUEnabled() { // Attach GPU support if requested
				h = types.Adapted(h, GPUAdapter)
			}

			lifetimeCtx, cancel := context.WithCancel(lifetimeCtx)
			exited, err := h(ctx, lifetimeCtx, wg, resp, req)
			if err != nil {
				cancel()
				return nil, err
			}

			job := &daemon.Job{
				JID:        jid,
				Type:       req.GetType(),
				Process:    &daemon.ProcessState{},
				GPUEnabled: req.GetGPUEnabled(),
				Details:    &daemon.Details{PID: proto.Uint32(resp.PID)},
			}

			// Get process state if only if possible, as the process
			// may have already exited by the time we get here.
			state := &daemon.ProcessState{}
			err = utils.FillProcessState(ctx, resp.PID, state)
			if err == nil {
				job.Process = state

				// Save job details
				err = db.PutJob(ctx, jid, job)
				if err != nil {
					cancel()
					return nil, status.Errorf(codes.Internal, "failed to save job details: %v", err)
				}

			}

			// Wait for process exit to update job
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-exited
				log.Info().Str("JID", jid).Uint32("PID", resp.PID).Msg("job exited")
				job.Process.Info = &daemon.ProcessInfo{IsRunning: false}
				ctx := context.WithoutCancel(ctx)
				err := db.PutJob(ctx, jid, job)
				if err != nil {
					log.Warn().Err(err).Str("JID", jid).Msg("failed to update job after exit")
				}
			}()

			return exited, nil
		}
	}
}
