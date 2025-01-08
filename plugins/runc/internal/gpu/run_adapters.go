package gpu

import (
	"context"
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

		// Check if GPU plugin is installed
		var gpu *plugins.Plugin
		if gpu = server.Plugins.Get("gpu"); gpu.Status != plugins.Installed {
			return nil, status.Errorf(
				codes.FailedPrecondition,
				"Please install the GPU plugin to use GPU support",
			)
		}

		libraryPath := gpu.LibraryPaths()[0]

		// HACK: Remove nvidia prestart hook as we don't support working around it, yet
		if spec.Hooks != nil {
			for i, hook := range spec.Hooks.Prestart {
				if strings.Contains(hook.Path, "nvidia") {
					spec.Hooks.Prestart = append(spec.Hooks.Prestart[:i], spec.Hooks.Prestart[i+1:]...)
					break
				}
			}
		}

		shmMount := &specs.Mount{}

		// Modify existing /dev/shm mount if it exists
		foundExisting := false
		for _, m := range spec.Mounts {
			if m.Destination == "/dev/shm" {
				foundExisting = true
				shmMount = &m
				break
			}
		}

		shmMount.Destination = "/dev/shm"
		shmMount.Source = "/dev/shm"
		shmMount.Type = "bind"
		shmMount.Options = []string{"rbind", "rprivate", "nosuid", "nodev", "rw"}

		if !foundExisting {
			spec.Mounts = append(spec.Mounts, *shmMount)
		}

		// Mount the GPU plugin library
		spec.Mounts = append(spec.Mounts, specs.Mount{
			Destination: libraryPath,
			Source:      libraryPath,
			Type:        "bind",
			Options:     []string{"rbind", "rpivate", "nosuid", "nodev", "rw"},
		})

		// Set the env vars
		if spec.Process == nil {
			return nil, status.Errorf(codes.FailedPrecondition, "spec does not have a process")
		}
		spec.Process.Env = append(spec.Process.Env, "LD_PRELOAD="+libraryPath)
		spec.Process.Env = append(spec.Process.Env, "CEDANA_JID="+req.JID)

		return next(ctx, server, resp, req)
	}
}
