package server

import (
	"context"

	"github.com/cedana/cedana/internal/plugins"
	"github.com/cedana/cedana/internal/server/adapters"
	"github.com/cedana/cedana/internal/server/handlers"
	"github.com/cedana/cedana/pkg/api/daemon"
	"github.com/cedana/cedana/pkg/types"
	"github.com/rs/zerolog/log"
	"github.com/spf13/viper"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *Server) Dump(ctx context.Context, req *daemon.DumpReq) (*daemon.DumpResp, error) {
	// Add basic adapters. The order below is the order followed before executing
	// the final handler (handlers.CriuDump). Post-dump, the order is reversed.

	compression := viper.GetString("storage.compression")

	middleware := []types.DumpAdapter{
		fillMissingDefaults,
		validateDumpRequest,
		adapters.PrepareDumpDir(compression),
		adapters.FillRequestIfJob,
		insertTypeSpecificMiddleware,
		adapters.FillProcessState, // need state right before dump
		adapters.DetectNetworkOptions,
		adapters.DetectShellJob,
		// adapters.CloseCommonFds,
	}

	handler := types.AdaptedDump(handlers.CriuDump(s.criu), middleware...)

	resp := &daemon.DumpResp{}
	err := handler(ctx, resp, req)
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
func fillMissingDefaults(h types.DumpHandler) types.DumpHandler {
	return func(ctx context.Context, resp *daemon.DumpResp, req *daemon.DumpReq) error {
		if req.GetDir() == "" {
			req.Dir = viper.GetString("storage.dump_dir")
		}

		if req.GetDetails().GetCriu() == nil {
			req.Details.Criu = &daemon.CriuOpts{
				LeaveRunning: viper.GetBool("criu.leave_running"),
			}
		} else {
			req.Details.Criu.LeaveRunning = viper.GetBool("criu.leave_running")
		}

		return h(ctx, resp, req)
	}
}

// Adapter that just checks all required fields are present in the request
func validateDumpRequest(h types.DumpHandler) types.DumpHandler {
	return func(ctx context.Context, resp *daemon.DumpResp, req *daemon.DumpReq) error {
		if req.GetDir() == "" {
			return status.Errorf(codes.InvalidArgument, "no dump dir specified")
		}
		if req.GetDetails() == nil {
			return status.Errorf(codes.InvalidArgument, "missing details")
		}
		if req.GetDetails().GetType() == "" {
			return status.Errorf(codes.InvalidArgument, "missing type")
		}

		return h(ctx, resp, req)
	}
}

// Adapter that inserts new adapters after itself based on the type of dump.
func insertTypeSpecificMiddleware(h types.DumpHandler) types.DumpHandler {
	return func(ctx context.Context, resp *daemon.DumpResp, req *daemon.DumpReq) error {
		middleware := []types.DumpAdapter{}
		t := req.GetDetails().GetType()
		switch t {
		case "job":
			// Insert adapters for job dump
			return status.Errorf(codes.InvalidArgument, "please first use fillRequestIfJob adapter for jobs")
		case "process":
			// Insert adapters for process dump
			middleware = append(middleware, adapters.CheckProcessExists)
		default:
			// Insert plugin-specific adapters
			if p, exists := plugins.LoadedPlugins[t]; exists {
				defer plugins.RecoverFromPanic(t)
				if dumpMiddleware, err := p.Lookup(plugins.FEATURE_DUMP_MIDDLEWARE); err == nil {
					middleware = append(middleware, dumpMiddleware.([]types.DumpAdapter)...)
				} else {
					return status.Errorf(codes.InvalidArgument, "plugin '%s' has no valid dump middleware: %v", t, err)
				}
			} else {
				return status.Errorf(codes.InvalidArgument, "unknown dump type: %s. maybe a missing plugin?", t)
			}
		}
		h = types.AdaptedDump(h, middleware...)
		return h(ctx, resp, req)
	}
}
