package adapters

// Defines all the adapters that manage the network details

import (
	"context"
	"syscall"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	criu_proto "buf.build/gen/go/cedana/criu/protocolbuffers/go/criu"
	"github.com/cedana/cedana/pkg/criu"
	"github.com/cedana/cedana/pkg/types"
	"github.com/cedana/cedana/pkg/utils"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

////////////////////////
//// Dump Adapters /////
////////////////////////

// Detect and sets network options for CRIU
// XXX YA: Enforces unsuitable options for CRIU. Some times, we may
// not want to use TCP established connections. Also, for external unix
// sockets, the flag is deprecated. The correct way is to use the
// --external flag in CRIU.
func DetectNetworkOptionsForDump(next types.Dump) types.Dump {
	return func(ctx context.Context, server types.ServerOpts, nfy *criu.NotifyCallbackMulti, resp *daemon.DumpResp, req *daemon.DumpReq) (chan int, error) {
		var hasTCP, hasExtUnixSocket bool

		if state := resp.GetState(); state != nil {
			for _, Conn := range state.GetInfo().GetOpenConnections() {
				if Conn.Type == syscall.SOCK_STREAM { // TCP
					hasTCP = true
				}
				if Conn.Type == syscall.AF_UNIX { // Interprocess
					hasExtUnixSocket = true
				}
			}

			activeTCP, err := utils.HasActiveTCPConnections(int32(state.GetPID()))
			if err != nil {
				return nil, status.Errorf(
					codes.Internal,
					"failed to check active TCP connections: %v",
					err,
				)
			}
			hasTCP = hasTCP || activeTCP
		} else {
			log.Warn().Msg("No process info found. it should have been filled by an adapter")
		}

		if req.GetCriu() == nil {
			req.Criu = &criu_proto.CriuOpts{}
		}

		// Only set unless already set
		if req.GetCriu().TcpEstablished == nil {
			req.Criu.TcpEstablished = proto.Bool(hasTCP)
		}
		if req.GetCriu().ExtUnixSk == nil {
			req.Criu.ExtUnixSk = proto.Bool(hasExtUnixSocket)
		}

		return next(ctx, server, nfy, resp, req)
	}
}

////////////////////////
/// Restore Adapters ///
////////////////////////

// Detect and sets network options for CRIU
// XXX YA: Enforces unsuitable options for CRIU. Some times, we may
// not want to use TCP established connections. Also, for external unix
// sockets, the flag is deprecated. The correct way is to use the
// --external flag in CRIU.
func DetectNetworkOptionsForRestore(next types.Restore) types.Restore {
	return func(ctx context.Context, server types.ServerOpts, nfy *criu.NotifyCallbackMulti, resp *daemon.RestoreResp, req *daemon.RestoreReq) (chan int, error) {
		var hasTCP, hasExtUnixSocket bool

		if state := resp.GetState(); state != nil {
			for _, Conn := range state.GetInfo().GetOpenConnections() {
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
		if req.GetCriu().TcpEstablished == nil {
			req.Criu.TcpEstablished = proto.Bool(hasTCP)
		}
		if req.GetCriu().ExtUnixSk == nil {
			req.Criu.ExtUnixSk = proto.Bool(hasExtUnixSocket)
		}

		return next(ctx, server, nfy, resp, req)
	}
}
