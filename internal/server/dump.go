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

func (s *Server) Dump(ctx context.Context, req *daemon.DumpReq) (*daemon.DumpResp, error) {
	// The order below is the order followed before executing
	// the final handler (handlers.Dump). Post-dump, the order is reversed.

	compression := config.Global.Storage.Compression

	middleware := types.Middleware[types.Dump]{
		defaults.FillMissingDumpDefaults,
		validation.ValidateDumpRequest,
		filesystem.PrepareDumpDir(compression),

		pluginDumpMiddleware, // middleware from plugins

		// Process state-dependent adapters
		process.FillProcessStateForDump,
		process.DetectShellJobForDump,
		process.CloseCommonFilesForDump,
		network.DetectNetworkOptionsForDump,

		validation.CheckCompatibilityForDump,
	}

	dump := criu.Dump.With(middleware...)

	if req.GetDetails().GetJID() != "" { // If using job dump
		dump = dump.With(job.ManageDump(s.jobs))
	}

	var profilingData *daemon.ProfilingData
	if config.Global.Profiling.Enabled {
		profilingData = &daemon.ProfilingData{Name: "dump"}
		defer profiling.RecordDuration(time.Now(), profilingData)
	}

	opts := types.ServerOpts{
		Lifetime:  s.lifetime,
		Plugins:   s.plugins,
		WG:        s.wg,
		Profiling: profilingData,
	}
	resp := &daemon.DumpResp{Profiling: profilingData}

	criu := criu.New[daemon.DumpReq, daemon.DumpResp](s.plugins)

	_, err := criu(dump)(ctx, opts, resp, req)
	if err != nil {
		return nil, err
	}

	log.Info().Str("path", resp.Path).Str("type", req.Type).Msg("dump successful")

	return resp, nil
}

//////////////////////////
//// Helper Adapters /////
//////////////////////////

// Adapter that inserts new adapters after itself based on the type of dump.
func pluginDumpMiddleware(next types.Dump) types.Dump {
	return func(ctx context.Context, server types.ServerOpts, resp *daemon.DumpResp, req *daemon.DumpReq) (exited chan int, err error) {
		middleware := types.Middleware[types.Dump]{}
		t := req.GetType()
		switch t {
		case "process":
			middleware = append(middleware, process.SetPIDForDump)
		default:
			// Insert plugin-specific middleware
			err = features.DumpMiddleware.IfAvailable(func(
				name string,
				pluginMiddleware types.Middleware[types.Dump],
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
