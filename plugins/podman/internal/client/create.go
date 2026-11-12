package client

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/plugins/podman"
	"github.com/cedana/cedana/pkg/config"
	"github.com/cedana/cedana/pkg/types"
	podman_keys "github.com/cedana/cedana/plugins/podman/pkg/keys"
	"github.com/cedana/cedana/plugins/podman/pkg/utils"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const OCI_RUNTIME_PLUGIN = "oci-runtime"

// Options for `podman create`. Built up by the middleware (e.g. the GPU adapters
// add annotations) and used by the handler to create the container, as a
// podman container's spec can't be updated once created.
type CreateOpts struct {
	Flags []string
	Image string
	Args  []string
}

// NOTE: values must not contain commas, as some podman versions split
// --annotation values on them
func (o *CreateOpts) Annotate(key, value string) {
	o.Flags = append(o.Flags, "--annotation", key+"="+value)
}

// Returns options to create a container that uses cedana-oci-runtime, which
// routes the container creation through cedana, using the given runtime plugin
// (e.g. crun).
func newCreateOpts(opts types.Opts, details *podman.Podman, runtime string) (*CreateOpts, error) {
	plugin := opts.Plugins.Get(OCI_RUNTIME_PLUGIN)
	if !plugin.IsInstalled() {
		return nil, status.Errorf(codes.FailedPrecondition, "please install the %s plugin", OCI_RUNTIME_PLUGIN)
	}
	ociRuntime := plugin.BinaryPaths()[0]

	cfg := config.Global
	cfg.Profiling.Enabled = false // NOTE: inner cedana shares the container's stdio, so would print profiling data into it

	configJson, err := json.Marshal(cfg)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to marshal config: %v", err)
	}

	executablePath, err := os.Executable()
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to get executable path: %v", err)
	}

	log.Debug().Str("oci_runtime", ociRuntime).Str("runtime", runtime).Msg("using cedana OCI runtime")

	createOpts := &CreateOpts{
		Flags: []string{"--name", details.ID, "--runtime", ociRuntime},
		Image: details.Image,
		Args:  details.Args,
	}

	createOpts.Annotate(podman_keys.RUNTIME_ANNOTATION, runtime)
	createOpts.Annotate(podman_keys.CONFIG_ANNOTATION, base64.StdEncoding.EncodeToString(configJson)) // For cedana-oci-runtime to call cedana with the current config
	createOpts.Annotate(podman_keys.EXECUTABLE_PATH_ANNOTATION, executablePath)                       // For cedana-oci-runtime to call cedana

	for _, env := range details.Env {
		createOpts.Flags = append(createOpts.Flags, "--env", env)
	}
	for _, gpu := range details.GPUs {
		createOpts.Flags = append(createOpts.Flags, "--device", fmt.Sprintf("nvidia.com/gpu=%d", gpu))
	}

	return createOpts, nil
}

// Returns the runtime plugin for podman's default runtime
func defaultRuntime(ctx context.Context) string {
	out, err := utils.Podman(ctx, "info", "--format", "{{.Host.OCIRuntime.Name}}")
	if err == nil {
		runtime := strings.TrimSpace(string(out))
		if utils.RootForPlugin(runtime) != "" {
			return runtime
		}
	}
	return utils.RUNTIMES[0].Plugin
}
