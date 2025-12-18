package client

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/config"
	"github.com/cedana/cedana/pkg/types"
	"github.com/cedana/cedana/pkg/utils"
	containerd_keys "github.com/cedana/cedana/plugins/containerd/pkg/keys"
	containerd_utils "github.com/cedana/cedana/plugins/containerd/pkg/utils"
	"github.com/containerd/containerd"
	"github.com/containerd/containerd/api/types/runc/options"
	"github.com/containerd/containerd/contrib/nvidia"
	"github.com/containerd/containerd/oci"
	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const waitForManageUpcomingTimeout = 2 * time.Minute

func CreateContainer(next types.Run) types.Run {
	return func(ctx context.Context, opts types.Opts, resp *daemon.RunResp, req *daemon.RunReq) (code func() <-chan int, err error) {
		details := req.GetDetails().GetContainerd()

		client, ok := ctx.Value(containerd_keys.CLIENT_CONTEXT_KEY).(*containerd.Client)
		if !ok {
			return nil, status.Errorf(codes.Internal, "failed to get client from context")
		}

		var container containerd.Container
		var image containerd.Image

		switch req.Action {
		case daemon.RunAction_START_NEW:

			image, err = client.GetImage(ctx, details.GetImage().GetName())
			if err != nil {
				return nil, status.Errorf(codes.Internal, "failed to get image: %v", err)
			}

			specOpts := []oci.SpecOpts{
				oci.WithImageConfig(image),
				oci.WithHostNamespace(specs.NetworkNamespace),
				oci.WithHostHostsFile,
				oci.WithHostResolvconf,
			}

			if len(details.Args) > 0 {
				specOpts = append(specOpts, oci.WithProcessArgs(details.Args...))
			}

			if len(details.GPUs) > 0 {
				specOpts = append(
					specOpts,
					nvidia.WithGPUs(
						nvidia.WithDevices(utils.Int32ToIntSlice(details.GPUs)...),
						nvidia.WithAllCapabilities,
					),
				)
			}

			configJson, err := json.Marshal(config.Global)
			if err != nil {
				return nil, status.Errorf(codes.Internal, "failed to marshal config: %v", err)
			}

			executablePath, err := os.Executable()
			if err != nil {
				return nil, status.Errorf(codes.Internal, "failed to get executable path: %v", err)
			}

			envVars := []string{
				"CEDANA_CONFIG=" + string(configJson),      // For the shim to call cedana with the current config
				"CEDANA_EXECUTABLE_PATH=" + executablePath, // For the shim to call cedana
			}
			specOpts = append(specOpts, oci.WithEnv(envVars))

			runtime := client.Runtime()
			pluginName := fmt.Sprintf("containerd/runtime-%s", containerd_utils.PluginForRuntime(runtime))

			// Check if the appropriate containerd runtime is installed

			plugin := opts.Plugins.Getf(pluginName)
			if !plugin.IsInstalled() {
				return nil, status.Errorf(codes.FailedPrecondition, "please install the %s plugin to run this container", pluginName)
			}
			newRuntime := plugin.BinaryPaths()[0]

			log.Debug().Str("current_runtime", runtime).Str("plugin", pluginName).Str("new_runtime", newRuntime).Msg("using cedana containerd runtime for run")

			container, err = client.NewContainer(
				ctx,
				details.ID,
				containerd.WithImage(image),
				containerd.WithNewSnapshot(details.ID, image),
				containerd.WithSnapshotter(details.Snapshotter),
				containerd.WithNewSpec(specOpts...),
				containerd.WithRuntime(newRuntime, &options.Options{}),
			)
			if err != nil {
				return nil, status.Errorf(codes.Internal, "failed to create container for run: %v", err)
			}
			defer func() {
				if err != nil {
					Cleanup(context.WithoutCancel(ctx), req.Details)
				}
			}()

			log.Debug().Str("id", container.ID()).Msg("created container for run")

		case daemon.RunAction_MANAGE_EXISTING:

			container, err = client.LoadContainer(ctx, details.ID)
			if err != nil {
				return nil, status.Errorf(codes.Internal, "failed to load container: %v", err)
			}

		case daemon.RunAction_MANAGE_UPCOMING:
		loop:
			for {
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-opts.Lifetime.Done():
					return nil, opts.Lifetime.Err()
				case <-time.After(500 * time.Millisecond):
					container, err = client.LoadContainer(ctx, details.ID)
					if err == nil {
						break loop
					}
					log.Trace().Str("id", details.ID).Msg("waiting for upcoming container to start managing")
				case <-time.After(waitForManageUpcomingTimeout):
					return nil, status.Errorf(codes.DeadlineExceeded, "timed out waiting for upcoming container %s", details.ID)
				}
			}
		}

		ctx = context.WithValue(ctx, containerd_keys.CONTAINER_CONTEXT_KEY, container)

		return next(ctx, opts, resp, req)
	}
}
