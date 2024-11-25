package server

import (
	"context"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/internal/server/adapters"
	"github.com/cedana/cedana/internal/server/handlers"
	"github.com/cedana/cedana/pkg/criu"
	"github.com/cedana/cedana/pkg/plugins"
	"github.com/cedana/cedana/pkg/types"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Pluggable features
const featureRestoreMiddleware plugins.Feature[types.Middleware[types.Restore]] = "RestoreMiddleware"

func (s *Server) Restore(ctx context.Context, req *daemon.RestoreReq) (*daemon.RestoreResp, error) {
	// Add basic adapters. The order below is the order followed before executing
	// the final handler (handlers.Restore). Post-restore, the order is reversed.

	middleware := types.Middleware[types.Restore]{
		// Bare minimum adapters
		adapters.ManageRestore(s.jobs),
		adapters.FillMissingRestoreDefaults,
		adapters.ValidateRestoreRequest,
		adapters.PrepareRestoreDir, // auto-detects compression

		pluginRestoreMiddleware, // middleware from plugins

		// Process state-dependent adapters
		adapters.FillProcessStateForRestore,
		adapters.DetectNetworkOptionsForRestore,
		adapters.DetectShellJobForRestore,
		adapters.InheritOpenFilesForRestore,
	}

	restore := handlers.RestoreCRIU().With(middleware...)

	opts := types.ServerOpts{
		Lifetime: s.lifetime,
		CRIU:     s.criu,
		Plugins:  s.plugins,
		WG:       s.wg,
	}
	resp := &daemon.RestoreResp{}

	// s.ctx is the lifetime context of the server, pass it so that
	// managed processes maximum lifetime is the same as the server.
	// It gives adapters the power to control the lifetime of the process. For e.g.,
	// the GPU adapter can use this context to kill the process when GPU support fails.
	_, err := restore(ctx, opts, &criu.NotifyCallbackMulti{}, resp, req)
	if err != nil {
		return nil, err
	}

	log.Info().Uint32("PID", resp.PID).Msg("restore successful")

	return resp, nil
}

//////////////////////////
//// Helper Adapters /////
//////////////////////////

// Adapter that inserts new adapters based on the type of restore request
func pluginRestoreMiddleware(next types.Restore) types.Restore {
	return func(ctx context.Context, server types.ServerOpts, nfy *criu.NotifyCallbackMulti, resp *daemon.RestoreResp, req *daemon.RestoreReq) (exited chan int, err error) {
		middleware := types.Middleware[types.Restore]{}
		t := req.GetType()
		switch t {
		case "process":
			// Nothing to do, yet
		default:
			// Insert plugin-specific middleware
			err = featureRestoreMiddleware.IfAvailable(func(
				name string,
				pluginMiddleware types.Middleware[types.Restore],
			) error {
				middleware = append(middleware, pluginMiddleware...)
				return nil
			})
			if err != nil {
				return nil, status.Errorf(codes.Unimplemented, err.Error())
			}
		}
		return next.With(middleware...)(ctx, server, nfy, resp, req)
	}
}
