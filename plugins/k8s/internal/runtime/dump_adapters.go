package runtime

import (
	"context"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/features"
	"github.com/cedana/cedana/pkg/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Checks for the runtime's plugin (e.g. containerd) and plugs in its dump middleware before calling
// the next handler. Also ensures settings the required request detauls for the runtime plugin.
func DumpMiddleware(next types.Dump) types.Dump {
	return func(ctx context.Context, opts types.Opts, resp *daemon.DumpResp, req *daemon.DumpReq) (exited chan int, err error) {

		err = features.DumpMiddleware.IfAvailable(func(_ string, runtimeMiddleware types.Middleware[types.Dump]) error {
			next = next.With(runtimeMiddleware...)
			return nil
		}, "containerd")
		if err != nil {
			return nil, status.Errorf(codes.FailedPrecondition, "unsupported runtime %s: %v", "containerd", err)
		}

		return next(ctx, opts, resp, req)
	}
}
