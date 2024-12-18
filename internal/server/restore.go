package server

import (
	"context"
	"slices"
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
	criu_client "github.com/cedana/cedana/pkg/criu"
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
		process.FillProcessStateForRestore,
		process.DetectShellJobForRestore,
		process.InheritStdioForRestore,
		network.DetectNetworkOptionsForRestore,

		validation.CheckCompatibilityForRestore,
	}

	if req.GetDetails().GetJID() != "" { // If using job restore
		middleware = slices.Insert(middleware, 0, job.ManageRestore(s.jobs))
	}

	var profilingData *daemon.ProfilingData
	if config.Global.Profiling.Enabled {
		profilingData = &daemon.ProfilingData{Name: "restore"}
		defer profiling.RecordDuration(time.Now(), profilingData)
	}

	opts := types.ServerOpts{
		Lifetime:     s.lifetime,
		CRIU:         s.criu,
		CRIUCallback: &criu_client.NotifyCallbackMulti{},
		Plugins:      s.plugins,
		WG:           s.wg,
		Profiling:    profilingData,
	}
	resp := &daemon.RestoreResp{Profiling: profilingData}

	// s.ctx is the lifetime context of the server, pass it so that
	// managed processes maximum lifetime is the same as the server.
	// It gives adapters the power to control the lifetime of the process. For e.g.,
	// the GPU adapter can use this context to kill the process when GPU support fails.
	_, err := criu.Restore.With(middleware...)(ctx, opts, resp, req)
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
