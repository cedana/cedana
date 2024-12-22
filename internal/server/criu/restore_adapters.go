package criu

import (
	"context"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Should ideally be called after all other adapters have run
// For now, checks if the installed CRIU version is compatible with the request
func CheckOptsForRestore(next types.Restore) types.Restore {
	return func(ctx context.Context, server types.ServerOpts, resp *daemon.RestoreResp, req *daemon.RestoreReq) (exited chan int, err error) {
		err = CheckOpts(ctx, server.CRIU, req.GetCriu())
		if err != nil {
			return nil, status.Errorf(codes.FailedPrecondition, "CRIU compatibility check failed: %v", err)
		}
		return next(ctx, server, resp, req)
	}
}
