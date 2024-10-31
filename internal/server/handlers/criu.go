package handlers

// Defines the CRIU handlers that ship with the server

import (
	"context"
	"fmt"

	"github.com/cedana/cedana/pkg/api/daemon"
	"github.com/cedana/cedana/pkg/types"
	"github.com/checkpoint-restore/go-criu/v7"
	"github.com/checkpoint-restore/go-criu/v7/rpc"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

const (
	CRIU_LOG_LEVEL = 0
	CRIU_LOG_FILE  = "criu.log"
)

// Returns a CRIU dump handler for the server
func CriuDump(c *criu.Criu) types.DumpHandler {
	return func(ctx context.Context, resp *daemon.DumpResp, req *daemon.DumpReq) error {
		opts := req.GetDetails().GetCriu()
		if opts == nil {
			return fmt.Errorf("criu options is nil")
		}

		version, err := c.GetCriuVersion()
		if err != nil {
			return status.Errorf(codes.Internal, "failed to get CRIU version: %v", err)
		}

		criuOpts := &rpc.CriuOpts{
			// Fixed opts
			LogFile:  proto.String(CRIU_LOG_FILE),
			LogLevel: proto.Int32(CRIU_LOG_LEVEL),

			// Variable opts
			Pid:         proto.Int32(int32(resp.GetState().GetPID())),
			ImagesDirFd: proto.Int32(opts.GetImagesDirFd()),
			// WorkDirFd:   proto.Int32(opts.GetWorkDirFd()),
			// ParentImg:      proto.String(opts.GetParentImg()),
			LeaveRunning:   proto.Bool(opts.GetLeaveRunning()),
			ExtUnixSk:      proto.Bool(opts.GetExtUnixSk()),
			TcpEstablished: proto.Bool(opts.GetTcpEstablished()),
			ShellJob:       proto.Bool(opts.GetShellJob()),
			FileLocks:      proto.Bool(opts.GetFileLocks()),
			EmptyNs:        proto.Uint32(opts.GetEmptyNs()),
			AutoDedup:      proto.Bool(opts.GetAutoDedup()),
			LazyPages:      proto.Bool(opts.GetLazyPages()),
			// StatusFd:       proto.Int32(opts.GetStatusFd()),
			// LsmProfile:      proto.String(opts.GetLsmProfile()),
			// LsmMountContext: proto.String(opts.GetLsmMountContext()),
			External: opts.GetExternal(),
		}

		log.Debug().Int("CRIU", version).Interface("opts", criuOpts).Msg("CRIU dump starting")

		err = c.Dump(criuOpts, criu.NoNotify{})
		if err != nil {
			return status.Errorf(codes.Internal, "failed CRIU dump: %v", err)
		}

		log.Debug().Int("CRIU", version).Msg("CRIU dump complete")

		return err
	}
}

// Returns a CRIU restore handler for the server
func CriuRestore(criu *criu.Criu) types.RestoreHandler {
	return func(ctx context.Context, resp *daemon.RestoreResp, req *daemon.RestoreReq) error {
		log.Debug().Msg("CRIU")
		opts := req.GetDetails().GetCriu()
		if opts == nil {
			return fmt.Errorf("criu options is nil")
		}

		return fmt.Errorf("not implemented")
	}
}
