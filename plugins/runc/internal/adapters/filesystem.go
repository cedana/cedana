package adapters

// Defines all the filesystem related adapters for the plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"buf.build/gen/go/cedana/criu/protocolbuffers/go/criu"
	"github.com/cedana/cedana/pkg/types"
	runc_keys "github.com/cedana/cedana/plugins/runc/pkg/keys"
	"github.com/opencontainers/runc/libcontainer"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

const extDescriptorsFilename = "descriptors.json"

///////////////////////
//// Dump Adapters ////
///////////////////////

// AddBindMountsForDump adds bind mounts as external mountpoints
func AddBindMountsForDump(next types.Dump) types.Dump {
	return func(ctx context.Context, server types.ServerOpts, resp *daemon.DumpResp, req *daemon.DumpReq) (chan int, error) {
		container, ok := ctx.Value(runc_keys.DUMP_CONTAINER_CONTEXT_KEY).(*libcontainer.Container)
		if !ok {
			return nil, status.Errorf(
				codes.FailedPrecondition,
				"failed to get container from context",
			)
		}

		criuOpts := req.GetCriu()
		if criuOpts == nil {
			criuOpts = &criu.CriuOpts{}
		}

		for _, m := range container.Config().Mounts {
			if m.IsBind() {
				criuOpts.External = append(
					criuOpts.External,
					fmt.Sprintf("mnt[%s]:%s", m.Destination, m.Destination),
				)
			}
		}

		return nil, nil
	}
}

func AddMaskedPathsForDump(next types.Dump) types.Dump {
	return func(ctx context.Context, server types.ServerOpts, resp *daemon.DumpResp, req *daemon.DumpReq) (chan int, error) {
		container, ok := ctx.Value(runc_keys.DUMP_CONTAINER_CONTEXT_KEY).(*libcontainer.Container)
		if !ok {
			return nil, status.Errorf(
				codes.FailedPrecondition,
				"failed to get container from context",
			)
		}

		criuOpts := req.GetCriu()
		if criuOpts == nil {
			criuOpts = &criu.CriuOpts{}
		}

		config := container.Config()
		state, err := container.State()
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to get container state: %v", err)
		}

		for _, path := range config.MaskPaths {
			fi, err := os.Stat(fmt.Sprintf("/proc/%d/root/%s", state.InitProcessPid, path))
			if err != nil {
				if os.IsNotExist(err) {
					continue
				}
				return nil, status.Errorf(codes.Internal, "failed to stat %s: %v", path, err)
			}
			if fi.IsDir() {
				continue
			}

			extMnt := &criu.ExtMountMap{
				Key: proto.String(path),
				Val: proto.String("/dev/null"),
			}
			criuOpts.ExtMnt = append(criuOpts.ExtMnt, extMnt)
		}

		return next(ctx, server, resp, req)
	}
}

func WriteExtDescriptorsForDump(next types.Dump) types.Dump {
	return func(ctx context.Context, server types.ServerOpts, resp *daemon.DumpResp, req *daemon.DumpReq) (exited chan int, err error) {
		container, ok := ctx.Value(runc_keys.DUMP_CONTAINER_CONTEXT_KEY).(*libcontainer.Container)
		if !ok {
			return nil, status.Errorf(
				codes.FailedPrecondition,
				"failed to get container from context",
			)
		}

		criuOpts := req.GetCriu()
		if criuOpts == nil {
			criuOpts = &criu.CriuOpts{}
		}

		state, err := container.State()
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to get container state: %v", err)
		}

		// Write the FD info to a file in the image directory
		fdsJSON, err := json.Marshal(state.ExternalDescriptors)
		if err != nil {
			return nil, status.Errorf(
				codes.Internal,
				"failed to marshal external descriptors: %v",
				err,
			)
		}

		err = os.WriteFile(
			filepath.Join(*criuOpts.ImagesDir, extDescriptorsFilename),
			fdsJSON,
			0o600,
		)
		if err != nil {
			return nil, status.Errorf(
				codes.Internal,
				"failed to write external descriptors: %v",
				err,
			)
		}

		exited, err = next(ctx, server, resp, req)
		if err == nil {
			return exited, nil
		}

		// Clean up the file if there was an error
		os.Remove(filepath.Join(*criuOpts.ImagesDir, extDescriptorsFilename))

		return exited, err
	}
}
