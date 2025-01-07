package network

import (
	"context"
	"syscall"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	criu_proto "buf.build/gen/go/cedana/criu/protocolbuffers/go/criu"
	"github.com/cedana/cedana/pkg/types"
	"github.com/rs/zerolog/log"
	"google.golang.org/protobuf/proto"
)

// Detect and sets network options for CRIU
// XXX YA: Enforces unsuitable options for CRIU. Some times, we may
// not want to use TCP established connections. Also, for external unix
// sockets, the flag is deprecated. The correct way is to use the
// --external flag in CRIU.
func DetectNetworkOptionsForRestore(next types.Restore) types.Restore {
	return func(ctx context.Context, server types.ServerOpts, resp *daemon.RestoreResp, req *daemon.RestoreReq) (chan int, error) {
		var hasTCP, hasExtUnixSocket bool

		if state := resp.GetState(); state != nil {
			for _, Conn := range state.GetOpenConnections() {
				if Conn.Type == syscall.SOCK_STREAM { // TCP
					hasTCP = true
				}
				if Conn.Type == syscall.AF_UNIX { // Interprocess
					hasExtUnixSocket = true
				}
			}
		} else {
			log.Warn().Msg("No process info found. it should have been filled by an adapter")
		}

		if req.GetCriu() == nil {
			req.Criu = &criu_proto.CriuOpts{}
		}

		// Only set unless already set
		req.Criu.TcpEstablished = proto.Bool(hasTCP)
		req.Criu.TcpClose = proto.Bool(hasTCP)
		req.Criu.ExtUnixSk = proto.Bool(hasExtUnixSocket)

		return next(ctx, server, resp, req)
	}
}
