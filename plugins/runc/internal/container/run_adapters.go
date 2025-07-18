package container

import (
	"context"
	"path/filepath"
	"strings"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/types"
	runc_keys "github.com/cedana/cedana/plugins/runc/pkg/keys"
	"github.com/cedana/cedana/plugins/runc/pkg/runc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// LoadSpecFromBundle loads the spec from the bundle path, and sets it in the context
func LoadSpecFromBundle(next types.Run) types.Run {
	return func(ctx context.Context, opts types.Opts, resp *daemon.RunResp, req *daemon.RunReq) (code func() <-chan int, err error) {
		if req.Action != daemon.RunAction_START_NEW {
			return next(ctx, opts, resp, req)
		}

		details := req.GetDetails().GetRunc()
		bundle := details.GetBundle()
		workingDir := details.GetWorkingDir()

		if !strings.HasPrefix(bundle, "/") { // if root path is not absolute
			bundle = filepath.Join(workingDir, bundle)
			details.Bundle = bundle
		}

		configFile := filepath.Join(bundle, runc.SpecConfigFile)

		spec, err := runc.LoadSpec(configFile)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to load spec: %v", err)
		}

		ctx = context.WithValue(ctx, runc_keys.SPEC_CONTEXT_KEY, spec)

		return next(ctx, opts, resp, req)
	}
}
