package criu

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/channel"
	"github.com/cedana/cedana/pkg/features"
	"github.com/cedana/cedana/pkg/keys"
	"github.com/cedana/cedana/pkg/logging"
	"github.com/cedana/cedana/pkg/profiling"
	"github.com/cedana/cedana/pkg/types"
	"github.com/cedana/cedana/pkg/utils"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

const CRIU_RESTORE_LOG_FILE = "criu-restore.log"

// Returns a CRIU restore handler for the server
func Restore(ctx context.Context, opts types.Opts, resp *daemon.RestoreResp, req *daemon.RestoreReq) (code func() <-chan int, err error) {
	if req.GetCriu() == nil {
		return nil, status.Error(codes.InvalidArgument, "criu options is nil")
	}

	version, err := opts.CRIU.GetCriuVersion(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to get CRIU version: %v", err)
	}

	log := log.With().Str("plugin", "CRIU").Int("version", version).Str("operation", "restore").Logger()

	criuOpts := req.GetCriu()

	// Set CRIU server
	criuOpts.LogFile = proto.String(CRIU_RESTORE_LOG_FILE)
	criuOpts.LogLevel = proto.Int32(logLevel())
	criuOpts.GhostLimit = proto.Uint32(GHOST_FILE_MAX_SIZE)
	criuOpts.LogToStderr = proto.Bool(false)

	// Change ownership of the dump directory
	uids := resp.GetState().GetUIDs()
	gids := resp.GetState().GetGIDs()
	if len(uids) == 0 || len(gids) == 0 {
		return nil, status.Error(codes.Internal, "missing UIDs/GIDs in process state")
	}
	err = utils.ChownAll(criuOpts.GetImagesDir(), int(uids[0]), int(gids[0]))
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to change ownership of dump directory: %v", err)
	}

	// NOTE: We don't handle reaping if the plugin has indicated that it's a 'reaper', assuming it will
	// handle it when and how it wants to.

	var exitCode chan int
	var ok bool
	reaper, _ := features.Reaper.IsAvailable(req.Type)
	if !reaper || req.Type == "process" {
		exitCode = make(chan int, 1)
	} else {
		exitCode, ok = ctx.Value(keys.EXIT_CODE_CHANNEL_CONTEXT_KEY).(chan int)
		if !ok {
			return nil, status.Errorf(codes.Internal, "exit code channel must be set by now since plugin '%s' is a reaper", req.Type)
		}
	}
	code = channel.Broadcaster(exitCode)

	log.Info().Msg("CRIU restore starting")
	log.Debug().Interface("opts", criuOpts).Msg("CRIU restore options")

	ctx, end := profiling.StartTimingCategory(ctx, "criu", opts.CRIU.Restore)

	criuResp, err := opts.CRIU.Restore(
		ctx,
		criuOpts,
		opts.CRIUCallback,
		opts.IO.Stdin,
		opts.IO.Stdout,
		opts.IO.Stderr,
		opts.ExtraFiles...,
	)

	end()

	logging.FromFile(
		log.WithContext(ctx),
		filepath.Join(criuOpts.GetImagesDir(), CRIU_RESTORE_LOG_FILE),
		zerolog.TraceLevel,
	)

	if err != nil {
		// NOTE: It's possible that after the restore failed, the process
		// exists as a zombie process. We need to reap it.
		if pid := resp.GetState().GetPID(); pid != 0 {
			if p, err := os.FindProcess(int(pid)); err == nil {
				p.Wait()
			}
		}

		return nil, status.Errorf(codes.Internal, "failed CRIU restore: %v", err)
	}
	resp.PID = uint32(*criuResp.Pid)

	if !reaper || req.Type == "process" {
		opts.WG.Go(func() {
			pid := int(resp.PID)
			log.Info().Int("PID", pid).Msg("starting process lifecycle monitor")
			p, _ := os.FindProcess(pid) // always succeeds on Linux
			waitStatus, waitErr := p.Wait()
			if waitErr != nil {
				log.Warn().Err(waitErr).Int("PID", pid).Msg("p.Wait() failed (expected for CRIU-restored processes); polling instead")
			} else {
				log.Warn().Int("PID", pid).Int("exitCode", waitStatus.ExitCode()).Msg("p.Wait() succeeded immediately — process exited before monitor started; this is unexpected for CRIU restores")
				exitCode <- waitStatus.ExitCode()
				close(exitCode)
				return
			}
			for {
				killErr := syscall.Kill(pid, 0)
				if killErr != nil {
					if killErr == syscall.ESRCH {
						log.Info().Int("PID", pid).Msg("process no longer exists (ESRCH); exiting monitor")
						break
					}
					log.Debug().Err(killErr).Int("PID", pid).Msg("Kill(0) returned non-ESRCH error — process still alive")
				}
				time.Sleep(500 * time.Millisecond)
			}

			log.Info().Int("PID", pid).Msg("job process has exited")
			exitCode <- 0
			close(exitCode)
		})
	}

	log.Info().Uint32("PID", resp.PID).Msg("CRIU restore complete")

	return code, err
}
