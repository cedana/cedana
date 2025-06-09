package cedana

import (
	"context"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/features"
	"github.com/cedana/cedana/pkg/profiling"
	"github.com/cedana/cedana/pkg/types"
	"github.com/cedana/cedana/pkg/utils"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *Server) RestoreVM(ctx context.Context, req *daemon.RestoreVMReq) (*daemon.RestoreVMResp, error) {
	middleware := types.Middleware[types.RestoreVM]{
		pluginRestoreVMMiddleware, // middleware from plugins

	}

	restore := pluginRestoreVMHandler().With(middleware...)

	opts := types.Opts{
		Lifetime: s.lifetime,
		Plugins:  s.plugins,
		WG:       s.wg,
		FdStore:  &s.fdStore,
	}
	resp := &daemon.RestoreVMResp{}

	_, err := restore(ctx, opts, resp, req)
	if err != nil {
		return nil, err
	}

	if utils.PathExists(resp.State) {
		log.Info().Str("vm state", resp.State).Str("type", req.Type).Msg("restore successful")
	}

	return resp, nil
}

// Adapter that inserts new adapters after itself based on the type of dump.
func pluginRestoreVMMiddleware(next types.RestoreVM) types.RestoreVM {
	return func(ctx context.Context, opts types.Opts, resp *daemon.RestoreVMResp, req *daemon.RestoreVMReq) (code func() <-chan int, err error) {
		middleware := types.Middleware[types.RestoreVM]{}
		t := req.GetType()
		switch t {
		default:
			// Insert plugin-specific middleware
			err = features.RestoreVMMiddleware.IfAvailable(func(
				name string,
				pluginMiddleware types.Middleware[types.RestoreVM],
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
func pluginRestoreVMHandler() types.RestoreVM {
	return func(ctx context.Context, opts types.Opts, resp *daemon.RestoreVMResp, req *daemon.RestoreVMReq) (code func() <-chan int, err error) {
		t := req.Type
		var handler types.RestoreVM
		switch t {
		default:
			// Use plugin-specific handler
			err = features.RestoreVMHandler.IfAvailable(func(name string, pluginHandler types.RestoreVM) error {
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
