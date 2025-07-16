package criu

import (
	"context"
	"io"
	"math/rand/v2"
	"os"
	"path/filepath"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/channel"
	"github.com/cedana/cedana/pkg/features"
	cedana_io "github.com/cedana/cedana/pkg/io"
	"github.com/cedana/cedana/pkg/keys"
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

var Restore types.Restore = restore

// Returns a CRIU restore handler for the server
func restore(ctx context.Context, opts types.Opts, resp *daemon.RestoreResp, req *daemon.RestoreReq) (code func() <-chan int, err error) {
	extraFiles, _ := ctx.Value(keys.EXTRA_FILES_CONTEXT_KEY).([]*os.File)

	if req.GetCriu() == nil {
		return nil, status.Error(codes.InvalidArgument, "criu options is nil")
	}

	version, err := opts.CRIU.GetCriuVersion(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to get CRIU version: %v", err)
	}

	criuOpts := req.GetCriu()

	// Set CRIU server
	criuOpts.LogFile = proto.String(CRIU_RESTORE_LOG_FILE)
	criuOpts.LogLevel = proto.Int32(CRIU_LOG_VERBOSITY_LEVEL)
	criuOpts.GhostLimit = proto.Uint32(GHOST_FILE_MAX_SIZE)
	criuOpts.NotifyScripts = proto.Bool(true)
	criuOpts.LogToStderr = proto.Bool(false)
	criuOpts.RstSibling = proto.Bool(true) // always restore as a child of the daemon

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

	log.Debug().Int("CRIU", version).Interface("opts", criuOpts).Msg("CRIU restore starting")

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

	var stdin io.Reader
	var stdout, stderr io.Writer

	// if we aren't using a client
	daemonless, _ := ctx.Value(keys.DAEMONLESS_CONTEXT_KEY).(bool)
	if daemonless {
		stdin = os.Stdin
		stdout = os.Stdout
		stderr = os.Stderr
	} else if req.Attachable {
		id := rand.Uint32() // Use a random number, since we don't have PID yet
		stdin, stdout, stderr = cedana_io.NewStreamIOSlave(opts.Lifetime, opts.WG, id, code())
		defer cedana_io.SetIOSlavePID(id, &resp.PID) // PID should be available then
	} else {
		outFile, ok := ctx.Value(keys.OUT_FILE_CONTEXT_KEY).(*os.File)
		if ok {
			stdout, stderr = outFile, outFile
		}
	}

	ctx, end := profiling.StartTimingCategory(ctx, "criu", opts.CRIU.Restore)

	criuResp, err := opts.CRIU.Restore(
		ctx,
		criuOpts,
		opts.CRIUCallback,
		stdin,
		stdout,
		stderr,
		extraFiles...)

	end()

	// Capture internal logs from CRIU
	utils.LogFromFile(
		log.With().Int("CRIU", version).Logger().WithContext(ctx),
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
		opts.WG.Add(1)
		go func() {
			defer opts.WG.Done()
			p, _ := os.FindProcess(int(resp.PID)) // always succeeds on linux
			status, err := p.Wait()
			if err != nil {
				log.Trace().Err(err).Msg("process Wait()")
			}
			exitCode <- status.ExitCode()
			close(exitCode)
		}()
	}

	log.Debug().Int("CRIU", version).Uint32("PID", resp.PID).Msg("CRIU restore complete")

	return code, err
}
