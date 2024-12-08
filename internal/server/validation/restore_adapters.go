package validation

import (
	"context"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/criu"
	"github.com/cedana/cedana/pkg/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Adapter that validates the restore request
func ValidateRestoreRequest(next types.Restore) types.Restore {
	return func(ctx context.Context, server types.ServerOpts, nfy *criu.NotifyCallbackMulti, resp *daemon.RestoreResp, req *daemon.RestoreReq) (exited chan int, err error) {
		if req.GetPath() == "" {
			return nil, status.Error(codes.InvalidArgument, "no path provided")
		}
		if req.GetType() == "" {
			return nil, status.Error(codes.InvalidArgument, "missing type")
		}

		return next(ctx, server, nfy, resp, req)
	}
}

// Should ideally be called after all other adapters have run
// For now, checks if the installed CRIU version is compatible with the request
func CheckCompatibilityForRestore(next types.Restore) types.Restore {
	return func(ctx context.Context, server types.ServerOpts, nfy *criu.NotifyCallbackMulti, resp *daemon.RestoreResp, req *daemon.RestoreReq) (exited chan int, err error) {
		err = CheckCRIUCompatibility(ctx, server.CRIU, req.GetCriu())
		if err != nil {
			return nil, status.Errorf(codes.FailedPrecondition, "CRIU compatibility check failed: %v", err)
		}
		return next(ctx, server, nfy, resp, req)
	}
}
