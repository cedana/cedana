package defaults

import (
	"context"
	"os"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	criu_proto "buf.build/gen/go/cedana/criu/protocolbuffers/go/criu"
	"github.com/cedana/cedana/pkg/config"
	"github.com/cedana/cedana/pkg/keys"
	"github.com/cedana/cedana/pkg/types"
	"google.golang.org/protobuf/proto"
)

// Adapter that fills missing info from the request using config defaults
func FillMissingRestoreDefaults(next types.Restore) types.Restore {
	return func(ctx context.Context, opts types.Opts, resp *daemon.RestoreResp, req *daemon.RestoreReq) (code func() <-chan int, err error) {
		if req.Criu == nil {
			req.Criu = &criu_proto.CriuOpts{}
		}

		if req.Criu.GetLeaveStopped() == false {
			req.Criu.LeaveStopped = proto.Bool(config.Global.CRIU.LeaveStopped)
		}

		req.Criu.NotifyScripts = proto.Bool(true)
		req.Criu.EvasiveDevices = proto.Bool(true)
		req.Criu.RstSibling = proto.Bool(true) // always restore as a child

		ctx = context.WithValue(ctx, keys.INHERIT_FD_MAP_CONTEXT_KEY, map[string]int32{})
		ctx = context.WithValue(ctx, keys.EXTRA_FILES_CONTEXT_KEY, []*os.File{})

		return next(ctx, opts, resp, req)
	}
}
