package client

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/config"
	"github.com/cedana/cedana/pkg/types"
	"github.com/cedana/cedana/pkg/utils"
	containerd_keys "github.com/cedana/cedana/plugins/containerd/pkg/keys"
	containerd_utils "github.com/cedana/cedana/plugins/containerd/pkg/utils"
	"github.com/containerd/containerd"
	"github.com/containerd/containerd/api/types/runc/options"
	"github.com/containerd/containerd/contrib/nvidia"
	"github.com/containerd/containerd/namespaces"
	"github.com/containerd/containerd/oci"
	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func SetupForRestore(next types.Restore) types.Restore {
	return func(ctx context.Context, opts types.Opts, resp *daemon.RestoreResp, req *daemon.RestoreReq) (code func() <-chan int, err error) {
		details := req.GetDetails().GetContainerd()

		ctx = namespaces.WithNamespace(ctx, details.Namespace)

		client, err := containerd.New(details.Address, containerd.WithDefaultNamespace(details.Namespace))
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to create containerd client: %v", err)
		}

		ctx = context.WithValue(ctx, containerd_keys.CLIENT_CONTEXT_KEY, client)

		return next(ctx, opts, resp, req)
	}
}

func CreateContainerForRestore(next types.Restore) types.Restore {
	return func(ctx context.Context, opts types.Opts, resp *daemon.RestoreResp, req *daemon.RestoreReq) (code func() <-chan int, err error) {
		details := req.GetDetails().GetContainerd()

		client, ok := ctx.Value(containerd_keys.CLIENT_CONTEXT_KEY).(*containerd.Client)
		if !ok {
			return nil, status.Errorf(codes.Internal, "failed to get client from context")
		}

		var container containerd.Container
		var image containerd.Image

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

		if len(details.GPUs) > 0 {
			specOpts = append(
				specOpts,
				nvidia.WithGPUs(
					nvidia.WithDevices(utils.Int32ToIntSlice(details.GPUs)...),
					nvidia.WithAllCapabilities,
				),
			)
		}

		req.Criu.InheritFd = nil // NOTE: Ignore previously set values as they will be added again by shim

		criuOptsJson, err := json.Marshal(req.Criu)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to marshal CRIU options: %v", err)
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
			"CEDANA_CHECKPOINT_PATH=" + req.Path,       // For the shim to know this is a restore and not a run
			"CEDANA_CRIU_OPTS=" + string(criuOptsJson), // For the shim to pass to `cedana restore <low-level runtime> ...`
			"CEDANA_EXECUTABLE_PATH=" + executablePath, // For the shim to call cedana
		}

		tmpfsMounts := []specs.Mount{}
		if len(details.Env) > 0 {
			for _, envVar := range details.Env {
				mountsStr, foundPersistEnv := strings.CutPrefix(envVar, "CEDANA_PERSISTENT_MOUNTS=")
				if foundPersistEnv {
					existingDests := map[string]bool{}
					for dest := range strings.SplitSeq(mountsStr, ",") {
						if dest == "" {
							continue
						}
						if existingDests[dest] {
							return nil, status.Errorf(codes.InvalidArgument, "cannot add persistent mount at %q: mount already exists", dest)
						}

						mount := specs.Mount{
							Destination: dest,
							Type:        "tmpfs",
							Source:      "tmpfs",
							Options:     []string{"nosuid", "strictatime", "mode=1777"},
						}
						tmpfsMounts = append(tmpfsMounts, mount)
						log.Debug().Str("destination", dest).Msg("added persistent tmpfs mount")
						existingDests[dest] = true
					}
				}
				specOpts = append(specOpts, oci.WithEnv([]string{envVar}))
			}
		}

		specOpts = append(specOpts, oci.WithMounts(tmpfsMounts))

		specOpts = append(specOpts, oci.WithEnv(envVars))

		// Read runtime from dump

		runtime := client.Runtime()

		file, err := opts.DumpFs.Open(containerd_keys.DUMP_RUNTIME_KEY)
		if err != nil {
			log.Warn().Err(err).Msgf("could not open runtime file from dump, will use %s", runtime)
		} else {
			defer file.Close()
			var runtimeBytes [256]byte
			n, err := file.Read(runtimeBytes[:])
			if err != nil {
				log.Warn().Err(err).Msgf("could not read runtime from dump, will use %s", runtime)
			} else {
				runtime = string(runtimeBytes[:n])
			}
		}

		pluginName := fmt.Sprintf("containerd/runtime-%s", containerd_utils.PluginForRuntime(runtime))

		// Check if the appropriate containerd runtime is installed

		plugin := opts.Plugins.Getf(pluginName)
		if !plugin.IsInstalled() {
			return nil, status.Errorf(codes.FailedPrecondition, "please install the %s plugin to restore this container", pluginName)
		}
		newRuntime := plugin.BinaryPaths()[0]

		log.Debug().Str("current_runtime", runtime).Str("plugin", pluginName).Str("new_runtime", newRuntime).Msg("using cedana containerd runtime for restore")

		container, err = client.NewContainer(
			ctx,
			details.ID,
			containerd.WithImage(image),
			containerd.WithNewSnapshot(details.ID+"-snapshot", image),
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

		log.Debug().Str("id", container.ID()).Msg("created container for restore")

		ctx = context.WithValue(ctx, containerd_keys.CONTAINER_CONTEXT_KEY, container)

		return next(ctx, opts, resp, req)
	}
}
