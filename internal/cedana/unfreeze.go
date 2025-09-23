package cedana

import (
	"context"
	"fmt"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/cedana/cedana/internal/cedana/defaults"
	"github.com/cedana/cedana/internal/cedana/gpu"
	"github.com/cedana/cedana/internal/cedana/job"
	"github.com/cedana/cedana/internal/cedana/process"
	"github.com/cedana/cedana/internal/cedana/validation"
	"github.com/cedana/cedana/pkg/criu"
	"github.com/cedana/cedana/pkg/features"
	"github.com/cedana/cedana/pkg/profiling"
	"github.com/cedana/cedana/pkg/types"

	"github.com/rs/zerolog/log"
)

func (s *Server) Unfreeze(ctx context.Context, req *daemon.DumpReq) (*daemon.DumpResp, error) {
	// NOTE: Unfreeze simply reuses the 'dump' req/resp and thus its adapters,
	// but the final handler is implemented by a plugin.

	// The order below is the order followed before executing
	// the final handler which is implemented by a plugin.

	middleware := types.Middleware[types.Unfreeze]{
		defaults.FillMissingUnfreezeDefaults,
		validation.ValidateUnfreezeRequest,

		pluginUnfreezeMiddleware, // middleware from plugins
		process.FillProcessStateForUnfreeze,
		gpu.Unfreeze(s.gpus),
	}

	unfreeze := pluginUnfreezeHandler().With(middleware...)

	if req.GetDetails().GetJID() != "" { // If using job unfreeze
		unfreeze = unfreeze.With(job.ManageUnfreeze(s.jobs))
	}

	opts := types.Opts{
		Lifetime:     s.lifetime,
		Plugins:      s.plugins,
		WG:           s.WaitGroup,
		CRIU:         criu.MakeCriu(),
		CRIUCallback: &criu.NotifyCallbackMulti{},
	}
	resp := &daemon.DumpResp{}

	_, err := unfreeze(ctx, opts, resp, req)
	if err != nil {
		log.Error().Err(err).Str("type", req.Type).Msg("unfreeze failed")
		return nil, err
	}

	log.Info().Str("type", req.Type).Msg("unfreeze successful")
	resp.Messages = append(resp.Messages, fmt.Sprintf("Unfroze %s PID %d", req.GetType(), resp.GetState().GetPID()))

	return resp, nil
}

//////////////////////////
//// Helper Adapters /////
//////////////////////////

var pluginUnfreezeMiddleware = pluginDumpMiddleware // Reuse dump middleware for unfreeze

// Handler that returns the type-specific handler for the unfreeze
func pluginUnfreezeHandler() types.Unfreeze {
	return func(ctx context.Context, opts types.Opts, resp *daemon.DumpResp, req *daemon.DumpReq) (code func() <-chan int, err error) {
		t := req.GetType()
		var handler types.Unfreeze
		switch t {
		case "process":
			handler = process.Unfreeze
		default:
			// Use plugin-specific handler
			err = features.UnfreezeHandler.IfAvailable(func(name string, pluginHandler types.Unfreeze) error {
				handler = pluginHandler
				return nil
			}, t)
			if err != nil {
				return nil, status.Error(codes.Unimplemented, err.Error())
			}
			var end func()
			ctx, end = profiling.StartTimingCategory(ctx, req.Type, handler)
			defer end()
		}
		return handler(ctx, opts, resp, req)
	}
}
