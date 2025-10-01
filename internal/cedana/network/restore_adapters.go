package network

import (
	"context"
	"syscall"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	criu_proto "buf.build/gen/go/cedana/criu/protocolbuffers/go/criu"
	"github.com/cedana/cedana/pkg/types"
	"github.com/cedana/cedana/pkg/utils"
	"github.com/rs/zerolog/log"
	"google.golang.org/protobuf/proto"
)

// Detect and sets network options for CRIU
// XXX YA: Enforces unsuitable options for CRIU. Some times, we may
// not want to use TCP established connections. Also, for external unix
// sockets, the flag is deprecated. The correct way is to use the
// --external flag in CRIU.
func DetectNetworkOptionsForRestore(next types.Restore) types.Restore {
	return func(ctx context.Context, opts types.Opts, resp *daemon.RestoreResp, req *daemon.RestoreReq) (code func() <-chan int, err error) {
		var hasTCP, hasExtUnixSocket bool

		if state := resp.GetState(); state != nil {
			utils.WalkTree(state, "OpenConnections", "Children", func(c *daemon.Connection) bool {
				if c.Type == syscall.SOCK_STREAM { // TCP
					hasTCP = true
				}
				if c.Type == syscall.AF_UNIX { // Interprocess
					hasExtUnixSocket = true
				}
				return true
			})
		} else {
			log.Warn().Msg("No process info found. it should have been filled by an adapter")
		}

		if req.GetCriu() == nil {
			req.Criu = &criu_proto.CriuOpts{}
		}

		// Only set unless already set
		req.Criu.TcpEstablished = proto.Bool(hasTCP || req.GetCriu().GetTcpEstablished())
		req.Criu.ExtUnixSk = proto.Bool(hasExtUnixSocket || req.GetCriu().GetExtUnixSk())

		return next(ctx, opts, resp, req)
	}
}
