package adapters

// This file contains all the adapters that manage container info

import (
	"context"
	"os"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/criu"
	"github.com/cedana/cedana/pkg/types"
	runc_keys "github.com/cedana/cedana/plugins/runc/pkg/keys"
	"github.com/cedana/cedana/plugins/runc/pkg/runc"
	"github.com/opencontainers/runc/libcontainer"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

//////////////////////
//// Run Adapters ////
//////////////////////

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

///////////////////////
//// Dump Adapters ////
///////////////////////

func GetContainerForDump(next types.Dump) types.Dump {
	return func(ctx context.Context, server types.ServerOpts, nfy *criu.NotifyCallbackMulti, resp *daemon.DumpResp, req *daemon.DumpReq) (chan int, error) {
		root := req.GetDetails().GetRunc().GetRoot()
		id := req.GetDetails().GetRunc().GetID()

		container, err := libcontainer.Load(root, id)
		if err != nil {
			return nil, status.Errorf(codes.NotFound, "failed to load container: %v", err)
		}

		ctx = context.WithValue(ctx, runc_keys.CONTAINER_CONTEXT_KEY, container)

		return next(ctx, server, nfy, resp, req)
	}
}

func SetPIDForDump(next types.Dump) types.Dump {
	return func(ctx context.Context, server types.ServerOpts, nfy *criu.NotifyCallbackMulti, resp *daemon.DumpResp, req *daemon.DumpReq) (chan int, error) {
		container, ok := ctx.Value(runc_keys.CONTAINER_CONTEXT_KEY).(*libcontainer.Container)
		if !ok {
			return nil, status.Errorf(codes.FailedPrecondition, "failed to get container from context")
		}

		state, err := container.State()
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to get container state: %v", err)
		}

		if resp.State == nil {
			resp.State = &daemon.ProcessState{}
		}
		resp.State.PID = uint32(state.InitProcessPid)

		return next(ctx, server, nfy, resp, req)
	}
}

//////////////////////////
//// Restore Adapters ////
//////////////////////////

func GetContainerForRestore(next types.Restore) types.Restore {
	return func(ctx context.Context, server types.ServerOpts, nfy *criu.NotifyCallbackMulti, resp *daemon.RestoreResp, req *daemon.RestoreReq) (chan int, error) {
		root := req.GetDetails().GetRunc().GetRoot()
		id := req.GetDetails().GetRunc().GetID()

		container, err := libcontainer.Load(root, id)
		if err != nil {
			return nil, status.Errorf(codes.NotFound, "failed to load container: %v", err)
		}

		ctx = context.WithValue(ctx, runc_keys.CONTAINER_CONTEXT_KEY, container)

		return next(ctx, server, nfy, resp, req)
	}
}
