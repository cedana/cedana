package handlers

// Defines the CRIU handlers that ship with the server

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/cedana/cedana/internal/utils"
	"github.com/cedana/cedana/pkg/api/daemon"
	"github.com/cedana/cedana/pkg/criu"
	"github.com/cedana/cedana/pkg/types"
	"github.com/checkpoint-restore/go-criu/v7/rpc"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

const (
	CRIU_LOG_VERBOSITY_LEVEL = 1
	CRIU_LOG_FILE            = "criu.log"
	GHOST_LIMIT              = 10000000
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
			LogFile:       proto.String(CRIU_LOG_FILE),
			LogLevel:      proto.Int32(CRIU_LOG_VERBOSITY_LEVEL),
			NotifyScripts: proto.Bool(true),

			// Variable opts
			Pid:            proto.Int32(int32(resp.GetState().GetPID())),
			ImagesDirFd:    proto.Int32(opts.GetImagesDirFd()),
			LeaveRunning:   proto.Bool(opts.GetLeaveRunning()),
			ExtUnixSk:      proto.Bool(opts.GetExtUnixSk()),
			TcpEstablished: proto.Bool(opts.GetTcpEstablished()),
			ShellJob:       proto.Bool(opts.GetShellJob()),
			FileLocks:      proto.Bool(opts.GetFileLocks()),
			EmptyNs:        proto.Uint32(opts.GetEmptyNs()),
			AutoDedup:      proto.Bool(opts.GetAutoDedup()),
			LazyPages:      proto.Bool(opts.GetLazyPages()),
			External:       opts.GetExternal(),
		}

		log.Debug().Int("CRIU", version).Interface("opts", criuOpts).Msg("CRIU dump starting")

		// Capture internal logs from CRIU
		logfile := filepath.Join(opts.GetImagesDir(), CRIU_LOG_FILE)
		ctx = log.With().Int("CRIU", version).Str("log", logfile).Logger().WithContext(ctx)
		go utils.ReadFileToLog(ctx, logfile)

		err = c.Dump(criuOpts, criu.NoNotify{})
		if err != nil {
			return status.Errorf(codes.Internal, "failed CRIU dump: %v", err)
		}

		log.Debug().Int("CRIU", version).Msg("CRIU dump complete")

		return err
	}
}

// Returns a CRIU restore handler for the server
func CriuRestore(c *criu.Criu) types.RestoreHandler {
	return func(ctx context.Context, resp *daemon.RestoreResp, req *daemon.RestoreReq) error {
		extraFiles, _ := ctx.Value(types.RESTORE_EXTRA_FILES_CONTEXT_KEY).([]*os.File)

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
			LogFile:         proto.String(CRIU_LOG_FILE),
			LogLevel:        proto.Int32(CRIU_LOG_VERBOSITY_LEVEL),
			LogToStderr:     proto.Bool(true),
			NotifyScripts:   proto.Bool(true),
			OrphanPtsMaster: proto.Bool(false),

			// Variable opts
			ImagesDirFd:    proto.Int32(opts.GetImagesDirFd()),
			TcpEstablished: proto.Bool(opts.GetTcpEstablished()),
			TcpClose:       proto.Bool(opts.GetTcpClose()),
			ShellJob:       proto.Bool(opts.GetShellJob()),
			FileLocks:      proto.Bool(opts.GetFileLocks()),
			EmptyNs:        proto.Uint32(opts.GetEmptyNs()),
			AutoDedup:      proto.Bool(opts.GetAutoDedup()),
			LazyPages:      proto.Bool(opts.GetLazyPages()),
			External:       opts.GetExternal(),
		}

		for _, f := range opts.GetInheritFd() {
			criuOpts.InheritFd = append(criuOpts.InheritFd, &rpc.InheritFd{
				Key: proto.String(f.GetKey()),
				Fd:  proto.Int32(f.GetFd()),
			})
		}

		log.Debug().Int("CRIU", version).Interface("opts", criuOpts).Msg("CRIU restore starting")

		// Capture internal logs from CRIU
		logfile := filepath.Join(opts.GetImagesDir(), CRIU_LOG_FILE)
		ctx = log.With().Int("CRIU", version).Str("log", logfile).Logger().WithContext(ctx)
		go utils.ReadFileToLog(ctx, logfile)

		err = c.Restore(criuOpts, criu.NotifyCallback{
			PreRestoreFunc: func() error {
				log.Info().Msg("PreRestoreFunc")
				return nil
			},
			OrphanPtsMasterFunc: func(fd int32) error {
				log.Info().Int32("fd", fd).Msg("OrphanPtsMasterFunc")
				return nil
			},
		}, extraFiles...)
		if err != nil {
			return status.Errorf(codes.Internal, "failed CRIU restore: %v", err)
		}

		log.Debug().Int("CRIU", version).Msg("CRIU restore complete")

		return err
	}
}
