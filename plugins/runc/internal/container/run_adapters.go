package container

import (
	"context"
	"os"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/types"
	runc_keys "github.com/cedana/cedana/plugins/runc/pkg/keys"
	"github.com/cedana/cedana/plugins/runc/pkg/runc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)
// LoadSpecFromBundle loads the spec from the bundle path, and sets it in the context
func LoadSpecFromBundle(next types.Run) types.Run {
	return func(ctx context.Context, server types.ServerOpts, resp *daemon.RunResp, req *daemon.RunReq) (chan int, error) {
		opts := req.GetDetails().GetRunc()
		bundle := opts.GetBundle()

		oldDir, err := os.Getwd()
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to get current working directory: %v", err)
		}
		err = os.Chdir(bundle)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to set working directory: %v", err)
		}
		defer os.Chdir(oldDir)

		spec, err := runc.LoadSpec(runc.SpecConfigFile)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to load spec: %v", err)
		}

		ctx = context.WithValue(ctx, runc_keys.SPEC_CONTEXT_KEY, spec)

		return next(ctx, server, resp, req)
	}
}
