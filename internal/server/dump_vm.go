package server

import (
	"context"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/internal/server/filesystem"
	"github.com/cedana/cedana/internal/server/validation"
	"github.com/cedana/cedana/internal/server/vm"
	"github.com/cedana/cedana/pkg/features"
	"github.com/cedana/cedana/pkg/types"
	"github.com/cedana/cedana/pkg/utils"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *Server) DumpVM(ctx context.Context, req *daemon.DumpVMReq) (*daemon.DumpVMResp, error) {

	middleware := types.Middleware[types.DumpVM]{
		validation.ValidateDumpVMRequest,
		filesystem.PrepareDumpVMDir,

		pluginDumpVMMiddleware, // middleware from plugins

	}

	dump := vm.Dump.With(middleware...)

	opts := types.Opts{
		Lifetime: s.lifetime,
		Plugins:  s.plugins,
		WG:       s.wg,
	}
	resp := &daemon.DumpVMResp{}

	vm := vm.New[daemon.DumpVMReq, daemon.DumpVMResp](s.plugins)

	_, err := vm(dump)(ctx, opts, resp, req)
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
	return func(ctx context.Context, opts types.Opts, resp *daemon.DumpVMResp, req *daemon.DumpVMReq) (exited chan int, err error) {
		middleware := types.Middleware[types.DumpVM]{}
		t := req.GetType()
		switch t {
		case "cloud-hypervisor":
			// nothing to do, we only support clh
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
