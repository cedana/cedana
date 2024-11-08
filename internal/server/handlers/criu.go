package handlers

// Defines the CRIU handlers that ship with the server

import (
	"context"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"sync"
	"syscall"

	"github.com/cedana/cedana/pkg/api/daemon"
	"github.com/cedana/cedana/pkg/criu"
	"github.com/cedana/cedana/pkg/types"
	"github.com/cedana/cedana/pkg/utils"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

const (
	CRIU_LOG_VERBOSITY_LEVEL = 4
	CRIU_LOG_FILE            = "criu.log"
	GHOST_FILE_MAX_SIZE      = 10000000 // 10MB
)

// Returns a CRIU dump handler for the server
func CriuDump(c *criu.Criu) types.DumpHandler {
	return func(ctx context.Context, wg *sync.WaitGroup, resp *daemon.DumpResp, req *daemon.DumpReq) error {
		opts := req.GetCriu()
		if opts == nil {
			return status.Error(codes.InvalidArgument, "criu options is nil")
		}

		version, err := c.GetCriuVersion()
		if err != nil {
			return status.Errorf(codes.Internal, "failed to get CRIU version: %v", err)
		}

		criuOpts := req.GetCriu()

		// Set CRIU opts
		criuOpts.LogFile = proto.String(CRIU_LOG_FILE)
		criuOpts.LogLevel = proto.Int32(CRIU_LOG_VERBOSITY_LEVEL)
		criuOpts.GhostLimit = proto.Uint32(GHOST_FILE_MAX_SIZE)
		criuOpts.Pid = proto.Int32(int32(resp.GetState().GetPID()))
		criuOpts.NotifyScripts = proto.Bool(true)

		log.Debug().Int("CRIU", version).Interface("opts", criuOpts).Msg("CRIU dump starting")

		// Capture internal logs from CRIU
		logfile := filepath.Join(opts.GetImagesDir(), CRIU_LOG_FILE)
		ctx = log.With().Int("CRIU", version).Str("log", logfile).Logger().WithContext(ctx)
		go utils.TraceFile(ctx, logfile)

		_, err = c.Dump(criuOpts, criu.NoNotify{})
		if err != nil {
			return status.Errorf(codes.Internal, "failed CRIU dump: %v", err)
		}

		log.Debug().Int("CRIU", version).Msg("CRIU dump complete")

		return err
	}
}

// Returns a CRIU restore handler for the server
func CriuRestore(c *criu.Criu) types.RestoreHandler {
	return func(ctx context.Context, lifetimeCtx context.Context, wg *sync.WaitGroup, resp *daemon.RestoreResp, req *daemon.RestoreReq) (chan int, error) {
		extraFiles := utils.GetContextValSafe(ctx, types.RESTORE_EXTRA_FILES_CONTEXT_KEY, []*os.File{})
		ioFiles := utils.GetContextValSafe(ctx, types.RESTORE_IO_FILES_CONTEXT_KEY, []*os.File{})

		opts := req.GetCriu()
		if opts == nil {
			return nil, status.Error(codes.InvalidArgument, "criu options is nil")
		}

		version, err := c.GetCriuVersion()
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to get CRIU version: %v", err)
		}

		criuOpts := req.GetCriu()

		// Set CRIU opts
		criuOpts.LogFile = proto.String(CRIU_LOG_FILE)
		criuOpts.LogLevel = proto.Int32(CRIU_LOG_VERBOSITY_LEVEL)
		criuOpts.GhostLimit = proto.Uint32(GHOST_FILE_MAX_SIZE)
		criuOpts.LogToStderr = proto.Bool(false)
		criuOpts.NotifyScripts = proto.Bool(true)
		criuOpts.OrphanPtsMaster = proto.Bool(false)

		log.Debug().Int("CRIU", version).Interface("opts", criuOpts).Msg("CRIU restore starting")

		// Capture internal logs from CRIU
		logfile := filepath.Join(opts.GetImagesDir(), CRIU_LOG_FILE)
		ctx = log.With().Int("CRIU", version).Str("log", logfile).Logger().WithContext(ctx)
		go utils.TraceFile(ctx, logfile)

		// Attach IO if requested, otherwise log to file
		exitCode := make(chan int, 1)
		var inWriter, outReader, errReader *os.File
		if req.Attach {
			if len(ioFiles) != 3 {
				return nil, status.Error(codes.Internal, "ioFiles did not contain 3 files")
			}
			inWriter, outReader, errReader = ioFiles[0], ioFiles[1], ioFiles[2]
			// Use a random number, since we don't have PID yet
			id := rand.Uint32()
			stdIn, stdOut, stdErr := utils.NewStreamIOSlave(lifetimeCtx, id, exitCode)
			defer utils.SetIOSlavePID(id, &resp.PID) // PID should be available then
			go io.Copy(inWriter, stdIn)
			go io.Copy(stdOut, outReader)
			go io.Copy(stdErr, errReader)
		}

		criuResp, err := c.Restore(criuOpts, criu.NoNotify{}, extraFiles...)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed CRIU restore: %v", err)
		}
		resp.PID = uint32(*criuResp.Pid)

		// If restoring as child of daemon (RstSibling), we need wait to close the exited channel
		// as their could be goroutines waiting on it.
		exited := make(chan int)
		if opts.GetRstSibling() {
			wg.Add(1)
			go func() {
				defer wg.Done()
				var status syscall.WaitStatus
				_, err := syscall.Wait4(int(resp.PID), &status, 0, nil)
				if err != nil {
					log.Debug().Err(err).Msg("process Wait4()")
				}
				code := status.ExitStatus()
				log.Debug().Int("code", status.ExitStatus()).Msg("process exited")
				exitCode <- code
				close(exitCode)
				close(exited)
			}()

			// Also kill the process if it's lifetime expires
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-lifetimeCtx.Done()
				syscall.Kill(int(resp.PID), syscall.SIGKILL)
			}()
		} else {
			close(exitCode)
			close(exited)
		}

		log.Debug().Int("CRIU", version).Msg("CRIU restore complete")

		return exited, err
	}
}
