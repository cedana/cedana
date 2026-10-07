package gpu

import (
	"context"
	"time"

	"github.com/cedana/cedana/pkg/keys"
	"github.com/cedana/cedana/pkg/types"
	"github.com/rs/zerolog/log"
)

// Adapter that detaches the job's GPU controller once the job exits, for chains without a job
// manager to do it (see job.Manage). Must come after the adapter that attaches the controller
// (Attach or Restore), which passes its ID down in the context.
func DetachOnExit[REQ, RESP any](gpus Manager) types.Adapter[types.Handler[REQ, RESP]] {
	return func(next types.Handler[REQ, RESP]) types.Handler[REQ, RESP] {
		return func(ctx context.Context, opts types.Opts, resp *RESP, req *REQ) (code func() <-chan int, err error) {
			code, err = next(ctx, opts, resp, req)
			if err != nil {
				return nil, err
			}

			id, ok := ctx.Value(keys.GPU_ID_CONTEXT_KEY).(string)
			if !ok || types.GPUID(req) != "" {
				return code, nil // no GPU, or a controller attached by someone else
			}

			// A detached job (runc --detach) outlives this command, so its controller is left
			// for the daemon to clean up when the job exits.
			if types.Details(req).GetRunc().GetDetach() {
				return code, nil
			}

			exited := code()

			opts.WG.Go(func() {
				<-exited

				// Terminating the controller waits for it to exit with no time limit, so stop
				// waiting after TERMINATE_TIMEOUT rather than keep the command from exiting.
				detached := make(chan error, 1)
				go func() { detached <- gpus.DetachByID(context.WithoutCancel(opts.Lifetime), id) }()

				select {
				case err := <-detached:
					if err != nil {
						log.Debug().Err(err).Str("ID", id).Msg("failed to detach GPU controller on exit")
					}
				case <-time.After(TERMINATE_TIMEOUT):
					log.Warn().Str("ID", id).Msg("timed out detaching GPU controller on exit")
				}
			})

			return code, nil
		}
	}
}
