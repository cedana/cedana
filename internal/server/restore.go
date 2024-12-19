package server

import (
	"context"
	"time"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/internal/features"
	"github.com/cedana/cedana/internal/server/criu"
	"github.com/cedana/cedana/internal/server/defaults"
	"github.com/cedana/cedana/internal/server/filesystem"
	"github.com/cedana/cedana/internal/server/job"
	"github.com/cedana/cedana/internal/server/network"
	"github.com/cedana/cedana/internal/server/process"
	"github.com/cedana/cedana/internal/server/validation"
	"github.com/cedana/cedana/pkg/config"
	"github.com/cedana/cedana/pkg/profiling"
	"github.com/cedana/cedana/pkg/types"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *Server) Restore(ctx context.Context, req *daemon.RestoreReq) (*daemon.RestoreResp, error) {
	// Add adapters. The order below is the order followed before executing
	// the final handler (handlers.Restore). Post-restore, the order is reversed.

	middleware := types.Middleware[types.Restore]{
		defaults.FillMissingRestoreDefaults,
		validation.ValidateRestoreRequest,
		filesystem.PrepareRestoreDir, // auto-detects compression

		pluginRestoreMiddleware, // middleware from plugins

		// Process state-dependent adapters
		process.ReloadProcessStateForRestore,
		process.DetectShellJobForRestore,
		process.InheritStdioForRestore,
		network.DetectNetworkOptionsForRestore,

		validation.CheckCompatibilityForRestore,
	}

	restore := criu.Restore.With(middleware...)

	if req.GetDetails().GetJID() != "" { // If using job restore
		restore = restore.With(job.ManageRestore(s.jobs))
	}

	restore = restore.With(criu.New[daemon.RestoreReq, daemon.RestoreResp](s.plugins)) // use a new instance per request

	var profilingData *daemon.ProfilingData
	if config.Global.Profiling.Enabled {
		profilingData = &daemon.ProfilingData{Name: "restore"}
		defer profiling.RecordDuration(time.Now(), profilingData)
	}

	opts := types.ServerOpts{
		Lifetime:  s.lifetime,
		Plugins:   s.plugins,
		WG:        s.wg,
		Profiling: profilingData,
	}
	resp := &daemon.RestoreResp{Profiling: profilingData}

	_, err := restore(ctx, opts, resp, req)
	if err != nil {
		return nil, err
	}

	log.Info().Uint32("PID", resp.PID).Str("type", req.Type).Msg("restore successful")

	return resp, nil
}

//////////////////////////
//// Helper Adapters /////
//////////////////////////

// Adapter that inserts new adapters based on the type of restore request
func pluginRestoreMiddleware(next types.Restore) types.Restore {
	return func(ctx context.Context, server types.ServerOpts, resp *daemon.RestoreResp, req *daemon.RestoreReq) (exited chan int, err error) {
		middleware := types.Middleware[types.Restore]{}
		t := req.GetType()
		switch t {
		case "process":
			// Nothing to do
		default:
			// Insert plugin-specific middleware
			err = features.RestoreMiddleware.IfAvailable(func(
				name string,
				pluginMiddleware types.Middleware[types.Restore],
			) error {
				middleware = append(middleware, pluginMiddleware...)
				return nil
			}, t)
			if err != nil {
				return nil, status.Error(codes.Unimplemented, err.Error())
			}
		}
		return next.With(middleware...)(ctx, server, resp, req)
	}
}
