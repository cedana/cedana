package filesystem

import (
	"context"
	"os"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func SetWorkingDirectory(next types.Run) types.Run {
	return func(ctx context.Context, server types.ServerOpts, resp *daemon.RunResp, req *daemon.RunReq) (chan int, error) {
		opts := req.GetDetails().GetRunc()
		workingDir := opts.GetWorkingDir()

		if workingDir != "" {
			oldDir, err := os.Getwd()
			if err != nil {
				return nil, status.Errorf(codes.Internal, "failed to get current working directory: %v", err)
			}
			err = os.Chdir(workingDir)
			if err != nil {
				return nil, status.Errorf(codes.Internal, "failed to set working directory: %v", err)
			}
			defer os.Chdir(oldDir)
		}

		return next(ctx, server, resp, req)
	}
}
