package server

import (
	"context"
	"sync"

	"github.com/cedana/cedana/internal/plugins"
	"github.com/cedana/cedana/internal/server/adapters"
	"github.com/cedana/cedana/internal/server/handlers"
	"github.com/cedana/cedana/pkg/api/criu"
	"github.com/cedana/cedana/pkg/api/daemon"
	"github.com/cedana/cedana/pkg/types"
	"github.com/rs/zerolog/log"
	"github.com/spf13/viper"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

func (s *Server) Dump(ctx context.Context, req *daemon.DumpReq) (*daemon.DumpResp, error) {
	// Add basic adapters. The order below is the order followed before executing
	// the final handler (handlers.CriuDump). Post-dump, the order is reversed.

	middleware := []types.Adapter[types.DumpHandler]{
		// Bare minimum adapters
		adapters.JobDumpAdapter(s.db),
		fillMissingDumpDefaults,
		validateDumpRequest,
		adapters.PrepareDumpDir(viper.GetString("storage.compression")),

		// Insert type-specific middleware, from plugins or built-in
		insertTypeSpecificDumpMiddleware,

		// Process state-dependent adapters
		adapters.FillProcessStateForDump,
		adapters.DetectNetworkOptionsForDump,
		adapters.DetectShellJobForDump,
		adapters.CloseCommonFilesForDump,
	}

	handler := types.Adapted(handlers.CriuDump(s.criu), middleware...)

	resp := &daemon.DumpResp{}
	err := handler(ctx, s.wg, resp, req)
	if err != nil {
		return nil, err
	}

	log.Info().Str("path", resp.Path).Msg("dump successful")

	return resp, nil
}

//////////////////////////
//// Helper Adapters /////
//////////////////////////

// Adapter that fills missing info from the request using config defaults
func fillMissingDumpDefaults(h types.DumpHandler) types.DumpHandler {
	return func(ctx context.Context, wg *sync.WaitGroup, resp *daemon.DumpResp, req *daemon.DumpReq) error {
		if req.GetDir() == "" {
			req.Dir = viper.GetString("storage.dump_dir")
		}

		if req.GetCriu() == nil {
			req.Criu = &criu.CriuOpts{}
		}

		// Only override if unset
		if req.GetCriu().LeaveRunning == nil {
			req.Criu.LeaveRunning = proto.Bool(viper.GetBool("criu.leave_running"))
		}

		return h(ctx, wg, resp, req)
	}
}

// Adapter that just checks all required fields are present in the request
func validateDumpRequest(h types.DumpHandler) types.DumpHandler {
	return func(ctx context.Context, wg *sync.WaitGroup, resp *daemon.DumpResp, req *daemon.DumpReq) error {
		if req.GetDir() == "" {
			return status.Errorf(codes.InvalidArgument, "no dump dir specified")
		}
		if req.GetDetails() == nil {
			return status.Errorf(codes.InvalidArgument, "missing details")
		}
		if req.GetType() == "" {
			return status.Errorf(codes.InvalidArgument, "missing type")
		}

		return h(ctx, wg, resp, req)
	}
}

// Adapter that inserts new adapters after itself based on the type of dump.
func insertTypeSpecificDumpMiddleware(h types.DumpHandler) types.DumpHandler {
	return func(ctx context.Context, wg *sync.WaitGroup, resp *daemon.DumpResp, req *daemon.DumpReq) error {
		middleware := []types.Adapter[types.DumpHandler]{}
		t := req.GetType()
		switch t {
		case "job":
			return status.Errorf(codes.InvalidArgument, "please first use JobDumpAdapter")
		case "process":
			// Insert adapters for process dump
			middleware = append(middleware, adapters.CheckProcessExistsForDump)
		default:
			// Insert plugin-specific middleware
			if p, exists := plugins.LoadedPlugins[t]; exists {
				defer plugins.RecoverFromPanic(t)
				if pluginMiddlewareUntyped, err := p.Lookup(plugins.FEATURE_DUMP_MIDDLEWARE); err == nil {
					pluginMiddleware, ok := pluginMiddlewareUntyped.([]types.Adapter[types.DumpHandler])
					if !ok {
						return status.Errorf(codes.InvalidArgument, "plugin '%s' has no valid dump middleware: %v", t, err)
					}
					middleware = append(middleware, pluginMiddleware...)
				} else {
					return status.Errorf(codes.InvalidArgument, "plugin '%s' has no valid dump middleware: %v", t, err)
				}
			} else {
				return status.Errorf(codes.InvalidArgument, "unknown dump type: %s. maybe a missing plugin?", t)
			}
		}
		h = types.Adapted(h, middleware...)
		return h(ctx, wg, resp, req)
	}
}
