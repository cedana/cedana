package defaults

import (
	"context"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	criu_proto "buf.build/gen/go/cedana/criu/protocolbuffers/go/criu"
	"github.com/cedana/cedana/pkg/types"
	"google.golang.org/protobuf/proto"
)

// Adapter that fills missing info from the request using config defaults
func FillMissingRestoreDefaults(next types.Restore) types.Restore {
	return func(ctx context.Context, opts types.Opts, resp *daemon.RestoreResp, req *daemon.RestoreReq) (code func() <-chan int, err error) {
		if req.Criu == nil {
			req.Criu = &criu_proto.CriuOpts{}
		}

		req.Criu.NotifyScripts = proto.Bool(true)
		req.Criu.EvasiveDevices = proto.Bool(true)
		req.Criu.RstSibling = proto.Bool(true) // always restore as a child

		return next(ctx, opts, resp, req)
	}
}
