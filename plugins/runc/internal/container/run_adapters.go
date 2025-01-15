package container

import (
	"context"
	"os"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/types"
	runc_keys "github.com/cedana/cedana/plugins/runc/pkg/keys"
	"github.com/cedana/cedana/plugins/runc/pkg/runc"
	"golang.org/x/sys/unix"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// LoadSpecFromBundle loads the spec from the bundle path, and sets it in the context
func LoadSpecFromBundle(next types.Run) types.Run {
	return func(ctx context.Context, opts types.Opts, resp *daemon.RunResp, req *daemon.RunReq) (chan int, error) {
		details := req.GetDetails().GetRunc()
		bundle := details.GetBundle()

		// If empty, assume cwd is the bundle. This is the behavior of runc binary as well.
		if bundle != "" {
			cwd, err := os.Getwd()
			if err != nil {
				return nil, status.Errorf(codes.Internal, "failed to get current working directory: %v", err)
			}
			err = os.Chdir(bundle)
			if err != nil {
				return nil, status.Errorf(codes.Internal, "failed to set working directory: %v", err)
			}
			defer os.Chdir(cwd)
		}

		spec, err := runc.LoadSpec(runc.SpecConfigFile)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to load spec: %v", err)
		}

		ctx = context.WithValue(ctx, runc_keys.SPEC_CONTEXT_KEY, spec)

		return next(ctx, opts, resp, req)
	}
}

// SetUsChildSubreaper sets the current process as the child subreaper
func SetUsChildSubreaper(next types.Run) types.Run {
	return func(ctx context.Context, opts types.Opts, resp *daemon.RunResp, req *daemon.RunReq) (chan int, error) {
		err := unix.Prctl(unix.PR_SET_CHILD_SUBREAPER, uintptr(1), 0, 0, 0)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to set us as child subreaper: %v", err)
		}

		return next(ctx, opts, resp, req)
	}
}
