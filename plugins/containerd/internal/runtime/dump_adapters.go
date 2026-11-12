package runtime

import (
	"context"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/plugins/runc"
	"github.com/cedana/cedana/pkg/features"
	"github.com/cedana/cedana/pkg/types"
	containerd_keys "github.com/cedana/cedana/plugins/containerd/pkg/keys"
	"github.com/cedana/cedana/plugins/containerd/pkg/utils"
	"github.com/containerd/containerd"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Checks for the runtime's plugin (e.g. runc) and plugs in its dump middleware before calling
// the next handler. Also ensures settings the required request detauls for the runtime plugin.
func DumpMiddleware(next types.Dump) types.Dump {
	return func(ctx context.Context, opts types.Opts, resp *daemon.DumpResp, req *daemon.DumpReq) (code func() <-chan int, err error) {
		details := req.GetDetails()
		id := details.GetContainerd().GetID()
		namespace := details.GetContainerd().GetNamespace()

		client, ok := ctx.Value(containerd_keys.CLIENT_CONTEXT_KEY).(*containerd.Client)
		if !ok {
			return nil, status.Errorf(codes.Internal, "failed to get containerd client from context")
		}

		plugin, runtime, binary, err := utils.PluginForContainer(ctx, client, id)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to get runtime info for container %s: %v", id, err)
		}

		if req.Action == daemon.DumpAction_DUMP && opts.DumpFs != nil {
			// Save runtime name in dump
			file, err := opts.DumpFs.Create(containerd_keys.DUMP_RUNTIME_KEY)
			if err != nil {
				return nil, status.Errorf(codes.Internal, "failed to create dump runtime file: %v", err)
			}
			defer file.Close()
			_, err = file.WriteString(runtime)
			if err != nil {
				return nil, status.Errorf(codes.Internal, "failed to write dump runtime file: %v", err)
			}

			// Save runtime binary in dump, so restore can use the same one
			if binary != "" {
				file, err := opts.DumpFs.Create(containerd_keys.DUMP_RUNTIME_BINARY_KEY)
				if err != nil {
					return nil, status.Errorf(codes.Internal, "failed to create dump runtime binary file: %v", err)
				}
				defer file.Close()
				_, err = file.WriteString(binary)
				if err != nil {
					return nil, status.Errorf(codes.Internal, "failed to write dump runtime binary file: %v", err)
				}
			}
		}

		err = features.DumpMiddleware.IfAvailable(func(_ string, runtimeMiddleware types.Middleware[types.Dump]) error {
			next = next.With(runtimeMiddleware...)
			return nil
		}, plugin)
		if err != nil {
			return nil, status.Errorf(codes.FailedPrecondition, "unsupported runtime %s: %v", runtime, err)
		}

		// Add runtime-specific details to the request

		switch plugin {
		case "runc", "crun":
			details.Runc = &runc.Runc{
				ID: id,
				// NOTE: the root is derived from the runtime (shim) name, not
				// the plugin name, as e.g. crun shares the runc shim and thus
				// its state root (/run/containerd/runc/<namespace>)
				Root: utils.RootFromRuntime(runtime, namespace),
			}
		default:
			return nil, status.Errorf(codes.Unimplemented, "unsupported plugin %s", plugin)
		}

		return next(ctx, opts, resp, req)
	}
}
