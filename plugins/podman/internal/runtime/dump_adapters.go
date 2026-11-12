package runtime

import (
	"context"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/plugins/runc"
	"github.com/cedana/cedana/pkg/features"
	"github.com/cedana/cedana/pkg/types"
	podman_keys "github.com/cedana/cedana/plugins/podman/pkg/keys"
	"github.com/cedana/cedana/plugins/podman/pkg/utils"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Checks for the runtime's plugin (e.g. crun) and plugs in its dump middleware before calling
// the next handler. Also ensures setting the required request details for the runtime plugin.
func DumpMiddleware(next types.Dump) types.Dump {
	return func(ctx context.Context, opts types.Opts, resp *daemon.DumpResp, req *daemon.DumpReq) (code func() <-chan int, err error) {
		container, ok := ctx.Value(podman_keys.CONTAINER_CONTEXT_KEY).(*utils.Container)
		if !ok {
			return nil, status.Errorf(codes.Internal, "failed to get container from context")
		}

		plugin, err := setRuntimeDetails(req, container)
		if err != nil {
			return nil, err
		}

		if req.Action == daemon.DumpAction_DUMP && opts.DumpFs != nil {
			// Save runtime in dump, so restore can use the same one
			file, err := opts.DumpFs.Create(podman_keys.DUMP_RUNTIME_KEY)
			if err != nil {
				return nil, status.Errorf(codes.Internal, "failed to create dump runtime file: %v", err)
			}
			defer file.Close()
			_, err = file.WriteString(plugin)
			if err != nil {
				return nil, status.Errorf(codes.Internal, "failed to write dump runtime file: %v", err)
			}
		}

		err = features.DumpMiddleware.IfAvailable(func(_ string, runtimeMiddleware types.Middleware[types.Dump]) error {
			next = next.With(runtimeMiddleware...)
			return nil
		}, plugin)
		if err != nil {
			return nil, status.Errorf(codes.FailedPrecondition, "unsupported runtime %s: %v", plugin, err)
		}

		return next(ctx, opts, resp, req)
	}
}

// Adds the runtime-specific details to the request, returning the runtime plugin
func setRuntimeDetails(req *daemon.DumpReq, container *utils.Container) (string, error) {
	plugin, root := utils.PluginForContainer(container)

	switch plugin {
	case "runc", "crun":
		req.Details.Runc = &runc.Runc{
			ID:   container.ID,
			Root: root,
		}
	default:
		return "", status.Errorf(codes.Unimplemented, "unsupported runtime %s", container.OCIRuntime)
	}

	return plugin, nil
}
