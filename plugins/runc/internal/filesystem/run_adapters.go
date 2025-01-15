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
	return func(ctx context.Context, opts types.Opts, resp *daemon.RunResp, req *daemon.RunReq) (chan int, error) {
		details := req.GetDetails().GetRunc()
		workingDir := details.GetWorkingDir()

		cwd, err := os.Getwd()
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to get current working directory: %v", err)
		}

		if workingDir != "" && workingDir != cwd {
			err = os.Chdir(workingDir)
			if err != nil {
				return nil, status.Errorf(codes.Internal, "failed to set working directory: %v", err)
			}
			defer os.Chdir(cwd)
		}

		return next(ctx, opts, resp, req)
	}
}
