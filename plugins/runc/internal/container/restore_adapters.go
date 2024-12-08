package container

import (
	"context"
	"os"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/criu"
	"github.com/cedana/cedana/pkg/types"
	runc_keys "github.com/cedana/cedana/plugins/runc/pkg/keys"
	"github.com/cedana/cedana/plugins/runc/pkg/runc"
	"github.com/opencontainers/runc/libcontainer"
	"github.com/opencontainers/runc/libcontainer/specconv"
	"github.com/opencontainers/runtime-spec/specs-go"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// LoadSpecFromBundleForRestore loads the spec from the bundle path, and sets it in the context
func LoadSpecFromBundleForRestore(next types.Restore) types.Restore {
	return func(ctx context.Context, server types.ServerOpts, nfy *criu.NotifyCallbackMulti, resp *daemon.RestoreResp, req *daemon.RestoreReq) (chan int, error) {
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

		return next(ctx, server, nfy, resp, req)
	}
}

func CreateContainerForRestore(next types.Restore) types.Restore {
	return func(ctx context.Context, server types.ServerOpts, nfy *criu.NotifyCallbackMulti, resp *daemon.RestoreResp, req *daemon.RestoreReq) (chan int, error) {
		root := req.GetDetails().GetRunc().GetRoot()
		id := req.GetDetails().GetRunc().GetID()

		spec, ok := ctx.Value(runc_keys.SPEC_CONTEXT_KEY).(*specs.Spec)
		if !ok {
			return nil, status.Errorf(codes.FailedPrecondition, "failed to get spec from context")
		}

		config, err := specconv.CreateLibcontainerConfig(&specconv.CreateOpts{
			CgroupName:      id,
			Spec:            spec,
			RootlessEUID:    false,
			RootlessCgroups: false,
		})
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to create libcontainer config: %v", err)
		}

		container, err := libcontainer.Create(root, id, config)
		if err != nil {
			return nil, status.Errorf(codes.NotFound, "failed to create container: %v", err)
		}
		ctx = context.WithValue(ctx, runc_keys.CONTAINER_CONTEXT_KEY, container)

		process, err := runc.NewProcess(*spec.Process)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to create new init process: %v", err)
		}
		process.Init = true
		ctx = context.WithValue(ctx, runc_keys.INIT_PROCESS_CONTEXT_KEY, process)

		return next(ctx, server, nfy, resp, req)
	}
}
