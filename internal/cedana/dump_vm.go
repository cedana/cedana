package cedana

import (
	"context"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/internal/cedana/defaults"
	"github.com/cedana/cedana/internal/cedana/validation"
	"github.com/cedana/cedana/pkg/features"
	"github.com/cedana/cedana/pkg/profiling"
	"github.com/cedana/cedana/pkg/types"
	"github.com/cedana/cedana/pkg/utils"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *Server) DumpVM(ctx context.Context, req *daemon.DumpVMReq) (*daemon.DumpVMResp, error) {
	middleware := types.Middleware[types.DumpVM]{
		defaults.FillMissingDumpVMDefaults,
		validation.ValidateDumpVMRequest,

		pluginDumpVMMiddleware, // middleware from plugins

	}

	dump := pluginDumpVMHandler().With(middleware...)

	opts := types.Opts{
		Lifetime: s.lifetime,
		Plugins:  s.plugins,
		WG:       s.wg,
	}
	resp := &daemon.DumpVMResp{}

	_, err := dump(ctx, opts, resp, req)
	if err != nil {
		return nil, err
	}

	if utils.PathExists(resp.TarDumpDir) {
		log.Info().Str("path", resp.TarDumpDir).Str("type", req.Type).Msg("dump successful")
	}

	return resp, nil
}

// Adapter that inserts new adapters after itself based on the type of dump.
func pluginDumpVMMiddleware(next types.DumpVM) types.DumpVM {
	return func(ctx context.Context, opts types.Opts, resp *daemon.DumpVMResp, req *daemon.DumpVMReq) (code func() <-chan int, err error) {
		middleware := types.Middleware[types.DumpVM]{}
		t := req.GetType()
		switch t {
		default:
			// Insert plugin-specific middleware
			err = features.DumpVMMiddleware.IfAvailable(func(
				name string,
				pluginMiddleware types.Middleware[types.DumpVM],
			) error {
				middleware = append(middleware, pluginMiddleware...)
				return nil
			}, t)
			if err != nil {
				return nil, status.Error(codes.Unimplemented, err.Error())
			}
		}
		return next.With(middleware...)(ctx, opts, resp, req)
	}
}

// Handler that returns the type-specific handler for the job
func pluginDumpVMHandler() types.DumpVM {
	return func(ctx context.Context, opts types.Opts, resp *daemon.DumpVMResp, req *daemon.DumpVMReq) (code func() <-chan int, err error) {
		t := req.Type
		var handler types.DumpVM
		switch t {
		default:
			// Use plugin-specific handler
			err = features.DumpVMHandler.IfAvailable(func(name string, pluginHandler types.DumpVM) error {
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
