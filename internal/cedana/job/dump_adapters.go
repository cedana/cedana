package job

import (
	"context"
	"fmt"
	"time"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/types"
	"github.com/rs/zerolog/log"
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

			job := jobs.Get(ctx, jid)
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

			taken := time.Now()

			code, err = next(ctx, opts, resp, req)
			if err != nil {
				return code, err
			}

			job.Sync(ctx, resp.GetState())

			// A restore of the job takes its latest saved checkpoint, so a path whose compress
			// or upload still runs is saved only once it has succeeded, in the background
			pending := make(map[string]bool, len(resp.GetPending()))
			if opts.Uploads != nil {
				for _, path := range resp.GetPending() {
					pending[path] = true
				}
			}
			var stored []string
			for _, path := range resp.GetPaths() {
				if !pending[path] {
					stored = append(stored, path)
					continue
				}
				waitCtx := context.WithoutCancel(ctx)
				opts.WG.Go(func() {
					result, err := opts.Uploads.Wait(waitCtx, path)
					if err == nil {
						err = result.Err
					}
					if err != nil {
						log.Error().Err(err).Str("jid", jid).Str("path", path).Msg("checkpoint not saved for the job: its upload failed")
						return
					}
					jobs.AddCheckpoint(jid, []string{path}, taken)
				})
			}
			jobs.AddCheckpoint(jid, stored, taken)

			// Wait for job exit & cleanup
			if !req.Criu.GetLeaveRunning() {
				if err := <-jobs.Done(jid); err != nil {
					resp.Messages = append(resp.Messages, err.Error())
				}
			}

			return code, nil
		}
	}
}
