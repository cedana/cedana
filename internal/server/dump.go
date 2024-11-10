package server

import (
	"context"
	"sync"

	"github.com/cedana/cedana/internal/config"
	"github.com/cedana/cedana/internal/plugins"
	"github.com/cedana/cedana/internal/server/adapters"
	"github.com/cedana/cedana/internal/server/handlers"
	"github.com/cedana/cedana/pkg/api/criu"
	"github.com/cedana/cedana/pkg/api/daemon"
	"github.com/cedana/cedana/pkg/types"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

func (s *Server) Dump(ctx context.Context, req *daemon.DumpReq) (*daemon.DumpResp, error) {
	// Add basic adapters. The order below is the order followed before executing
	// the final handler (handlers.CriuDump). Post-dump, the order is reversed.

	compression := config.Get(config.STORAGE_COMPRESSION)

	middleware := []types.Adapter[types.DumpHandler]{
		// Bare minimum adapters
		adapters.JobDumpAdapter(s.db),
		fillMissingDumpDefaults,
		validateDumpRequest,
		adapters.PrepareDumpDir(compression),

		pluginDumpMiddleware, // middleware from plugins

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
			req.Dir = config.Get(config.STORAGE_DUMP_DIR)
		}

		if req.GetCriu() == nil {
			req.Criu = &criu.CriuOpts{}
		}

		// Only override if unset
		if req.GetCriu().LeaveRunning == nil {
			req.Criu.LeaveRunning = proto.Bool(config.Get(config.CRIU_LEAVE_RUNNING))
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
func pluginDumpMiddleware(h types.DumpHandler) types.DumpHandler {
	return func(ctx context.Context, wg *sync.WaitGroup, resp *daemon.DumpResp, req *daemon.DumpReq) (err error) {
		middleware := []types.Adapter[types.DumpHandler]{}
		t := req.GetType()
		switch t {
		case "process":
			// Insert adapters for process dump
			middleware = append(middleware, adapters.CheckProcessExistsForDump)
		default:
			// Insert plugin-specific middleware
			err = plugins.IfFeatureAvailable(plugins.FEATURE_DUMP_MIDDLEWARE, func(
				name string,
				pluginMiddleware []types.Adapter[types.DumpHandler],
			) error {
				middleware = append(middleware, pluginMiddleware...)
				return nil
			})
			if err != nil {
				return status.Errorf(codes.Unimplemented, err.Error())
			}
		}
		h = types.Adapted(h, middleware...)
		return h(ctx, wg, resp, req)
	}
}
