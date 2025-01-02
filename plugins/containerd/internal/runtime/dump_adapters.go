package runtime

import (
	"context"
	"path/filepath"
	"strings"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/plugins/runc"
	"github.com/cedana/cedana/pkg/features"
	"github.com/cedana/cedana/pkg/types"
	"github.com/cedana/cedana/plugins/containerd/internal/defaults"
	containerd_keys "github.com/cedana/cedana/plugins/containerd/pkg/keys"
	"github.com/containerd/containerd"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Checks for the runtime's plugin (e.g. runc) and plugs in its dump middleware before calling
// the next handler. Also ensures settings the required request detauls for the runtime plugin.
func DumpMiddleware(next types.Dump) types.Dump {
	return func(ctx context.Context, server types.ServerOpts, resp *daemon.DumpResp, req *daemon.DumpReq) (exited chan int, err error) {
		details := req.GetDetails()
		id := details.GetContainerd().GetID()
		namespace := details.GetContainerd().GetNamespace()

		client, ok := ctx.Value(containerd_keys.CLIENT_CONTEXT_KEY).(*containerd.Client)
		if !ok {
			return nil, status.Errorf(codes.Internal, "failed to get containerd client from context")
		}

		runtime := client.Runtime()
		plugin, err := pluginForRuntime(runtime)
		if err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "failed to determine plugin for runtime: %v", err)
		}

		err = features.DumpMiddleware.IfAvailable(func(_ string, runtimeMiddleware types.Middleware[types.Dump]) error {
			next = next.With(runtimeMiddleware...)
			return nil
		}, plugin)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to plug in %s runtime middleware: %v", plugin, err)
		}

		// Add runtime-specific details to the request

		switch plugin {
		case "runc":
			details.Runc = &runc.Runc{
				ID:   id,
				Root: rootFromPlugin(plugin, namespace),
			}
		default:
			return nil, status.Errorf(codes.Unimplemented, "unsupported containerd runtime %s", runtime)
		}

		return next(ctx, server, resp, req)
	}
}

// E.g. io.containerd.runc.v2 -> runc
func pluginForRuntime(runtime string) (string, error) {
	parts := strings.Split(runtime, ".")
	if len(parts) < 3 {
		return "", status.Errorf(codes.InvalidArgument, "unrecognized runtime format: %s", runtime)
	}

	return parts[2], nil
}

// Get the root runtime directory for the namespace (e.g. runc)
// E.g. /run/containerd/runc/default
func rootFromPlugin(plugin, namespace string) string {
	return filepath.Join(defaults.BASE_RUNTIME_DIR, plugin, namespace)
}
