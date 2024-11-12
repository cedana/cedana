package server

import (
	"context"

	"github.com/cedana/cedana/internal/config"
	"github.com/cedana/cedana/internal/plugins"
	"github.com/cedana/cedana/internal/server/adapters"
	"github.com/cedana/cedana/internal/server/handlers"
	"github.com/cedana/cedana/pkg/api/daemon"
	"github.com/cedana/cedana/pkg/types"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *Server) Dump(ctx context.Context, req *daemon.DumpReq) (*daemon.DumpResp, error) {
	// Add basic adapters. The order below is the order followed before executing
	// the final handler (handlers.Dump). Post-dump, the order is reversed.

	compression := config.Get(config.STORAGE_COMPRESSION)

	middleware := types.Middleware[types.Dump]{
		// Bare minimum adapters
		adapters.JobDumpAdapter(s.db),
		adapters.FillMissingDumpDefaults,
		adapters.ValidateDumpRequest,
		adapters.PrepareDumpDir(compression),

		pluginDumpMiddleware, // middleware from plugins

		// Process state-dependent adapters
		adapters.FillProcessStateForDump,
		adapters.DetectNetworkOptionsForDump,
		adapters.DetectShellJobForDump,
		adapters.CloseCommonFilesForDump,
	}

	dump := handlers.Dump(s.ctx, s.wg, s.criu).With(middleware...)

	resp := &daemon.DumpResp{}
	err := dump.Handle(ctx, resp, req)
	if err != nil {
		return nil, err
	}

	log.Info().Str("path", resp.Path).Msg("dump successful")

	return resp, nil
}

//////////////////////////
//// Helper Adapters /////
//////////////////////////

// Adapter that inserts new adapters after itself based on the type of dump.
func pluginDumpMiddleware(next types.Handler[types.Dump]) types.Handler[types.Dump] {
	next.Handle = func(ctx context.Context, resp *daemon.DumpResp, req *daemon.DumpReq) (err error) {
		middleware := types.Middleware[types.Dump]{}
		t := req.GetType()
		switch t {
		case "process":
			// Insert adapters for process dump
			middleware = append(middleware, adapters.CheckProcessExistsForDump)
		default:
			// Insert plugin-specific middleware
			err = plugins.IfFeatureAvailable(plugins.FEATURE_DUMP_MIDDLEWARE, func(
				name string,
				pluginMiddleware types.Middleware[types.Dump],
			) error {
				middleware = append(middleware, pluginMiddleware...)
				return nil
			})
			if err != nil {
				return status.Errorf(codes.Unimplemented, err.Error())
			}
		}
		return next.With(middleware...).Handle(ctx, resp, req)
	}
	return next
}
