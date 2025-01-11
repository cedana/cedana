package gpu

import (
	"context"
	"fmt"
	"strings"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/plugins"
	"github.com/cedana/cedana/pkg/types"
	runc_keys "github.com/cedana/cedana/plugins/runc/pkg/keys"
	"github.com/opencontainers/runtime-spec/specs-go"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Adapter that adds Cedana GPU interception to the container.
// Modifies the spec as necessary.
func Interception(next types.Run) types.Run {
	return func(ctx context.Context, server types.ServerOpts, resp *daemon.RunResp, req *daemon.RunReq) (chan int, error) {
		spec, ok := ctx.Value(runc_keys.SPEC_CONTEXT_KEY).(*specs.Spec)
		if !ok {
			return nil, status.Errorf(codes.Internal, "failed to get spec from context")
		}

		jid := req.JID
		if jid == "" {
			return nil, status.Errorf(codes.InvalidArgument, "JID is required for GPU interception")
		}

		// Check if GPU plugin is installed
		var gpu *plugins.Plugin
		if gpu = server.Plugins.Get("gpu"); gpu.Status != plugins.Installed {
			return nil, status.Errorf(
				codes.FailedPrecondition,
				"Please install the GPU plugin to use GPU support",
			)
		}

		libraryPath := gpu.LibraryPaths()[0]

		err := AddGPUInterceptionToSpec(spec, libraryPath, jid)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to add GPU interception to spec: %v", err)
		}

		return next(ctx, server, resp, req)
	}
}

///////////////////////////
//// Helper Functions ////
///////////////////////////

func AddGPUInterceptionToSpec(spec *specs.Spec, libraryPath string, jid string) error {
	// HACK: Remove nvidia prestart hook as we don't support working around it, yet
	if spec.Hooks != nil {
		for i, hook := range spec.Hooks.Prestart {
			if strings.Contains(hook.Path, "nvidia") {
				spec.Hooks.Prestart = append(spec.Hooks.Prestart[:i], spec.Hooks.Prestart[i+1:]...)
				break
			}
		}
	}

	// Remove existing /dev/shm mount if it exists
	for i, m := range spec.Mounts {
		if m.Destination == "/dev/shm" {
			spec.Mounts = append(spec.Mounts[:i], spec.Mounts[i+1:]...)
		}
	}

	spec.Mounts = append(spec.Mounts, specs.Mount{
		Destination: "/dev/shm",
		Source:      "/dev/shm",
		Type:        "bind",
		Options:     []string{"rbind", "nosuid", "nodev", "rw"},
	})

	// Mount the GPU plugin library
	spec.Mounts = append(spec.Mounts, specs.Mount{
		Destination: libraryPath,
		Source:      libraryPath,
		Type:        "bind",
		Options:     []string{"rbind", "nosuid", "nodev", "rw"},
	})

	// Set the env vars
	if spec.Process == nil {
		return fmt.Errorf("spec does not have a process")
	}
	spec.Process.Env = append(spec.Process.Env, "LD_PRELOAD="+libraryPath)
	spec.Process.Env = append(spec.Process.Env, "CEDANA_JID="+jid)

	return nil
}
