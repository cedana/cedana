package defaults

import (
	"context"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	criu_proto "buf.build/gen/go/cedana/criu/protocolbuffers/go/criu"
	"github.com/cedana/cedana/pkg/config"
	"github.com/cedana/cedana/pkg/types"
	"google.golang.org/protobuf/proto"
)

// Adapter that fills missing info from the request using config defaults
func FillMissingDumpDefaults(next types.Dump) types.Dump {
	return func(ctx context.Context, server types.ServerOpts, resp *daemon.DumpResp, req *daemon.DumpReq) (exited chan int, err error) {
		if req.GetDir() == "" {
			req.Dir = config.Get(config.STORAGE_DUMP_DIR)
		}

		if req.GetCriu() == nil {
			req.Criu = &criu_proto.CriuOpts{}
		}

		// Only override if unset
		if req.GetCriu().LeaveRunning == nil {
			req.Criu.LeaveRunning = proto.Bool(config.Get(config.CRIU_LEAVE_RUNNING))
		}

		return next(ctx, server, resp, req)
	}
}
