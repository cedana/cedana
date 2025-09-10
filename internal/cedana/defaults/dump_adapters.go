package defaults

import (
	"context"
	"fmt"
	"strings"
	"time"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	criu_proto "buf.build/gen/go/cedana/criu/protocolbuffers/go/criu"
	"github.com/cedana/cedana/pkg/config"
	"github.com/cedana/cedana/pkg/types"
	"google.golang.org/protobuf/proto"
)

// Adapter that fills missing info from the request using config defaults
func FillMissingDumpDefaults(next types.Dump) types.Dump {
	return func(ctx context.Context, opts types.Opts, resp *daemon.DumpResp, req *daemon.DumpReq) (code func() <-chan int, err error) {
		if req.Dir == "" {
			req.Dir = config.Global.Checkpoint.Dir
		}

		if req.Name == "" {
			req.Name = fmt.Sprintf("dump-%s-%d", req.GetType(), time.Now().UnixNano())
		}

		if req.Criu == nil {
			req.Criu = &criu_proto.CriuOpts{}
		}

		// Only override if unset
		if req.Criu.GetLeaveRunning() == false {
			req.Criu.LeaveRunning = proto.Bool(config.Global.CRIU.LeaveRunning)
		}

		// Only override if unset
		if req.Criu.ManageCgroupsMode == nil {
			mode := criu_proto.CriuCgMode(criu_proto.CriuCgMode_value[strings.ToUpper(config.Global.CRIU.ManageCgroups)])
			req.Criu.ManageCgroupsMode = &mode
			req.Criu.ManageCgroups = proto.Bool(true) // For backward compatibility
		}

		return next(ctx, opts, resp, req)
	}
}
