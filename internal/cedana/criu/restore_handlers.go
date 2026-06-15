package criu

import (
	"context"
	"os"
	"path/filepath"

	"sort"
	"strconv"

	"time"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"buf.build/gen/go/cedana/criu/protocolbuffers/go/criu"
	"github.com/cedana/cedana/pkg/channel"
	"github.com/cedana/cedana/pkg/config"
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

func addRestoreProfiling(ctx context.Context, criuResp *criu.CriuRestoreResp) {
	if criuResp == nil || criuResp.GetStats() == nil {
		return
	}

	stats := criuResp.GetStats()
	preRestoreTime := time.Duration(stats.GetPreRestoreTime()) * time.Microsecond
	profiling.AddTimingParallelComponent(ctx, preRestoreTime, "PreRestore")

	restoreTime := time.Duration(stats.GetRestoreTime()) * time.Microsecond
	restoreCtx := profiling.AddTimingParallelComponent(ctx, restoreTime, "Restore")
	type pidTimeEntry struct {
		time  uint32
		pid   uint32
		stage string
	}

	var timingsByStage [][]pidTimeEntry

	for _, processRestoreStats := range stats.GetProcessRestoreStats() {
		// the order of time entries is perserved in when deserializing
		// protobufs, so this allows us to index using i
		// and all ProcessRestoreStats will have the same number of timeEntries
		timingsByStage = make([][]pidTimeEntry, len(processRestoreStats.GetTimeEntries()))
		for i, timeEntry := range processRestoreStats.GetTimeEntries() {
			timingsByStage[i] = append(timingsByStage[i], pidTimeEntry{timeEntry.GetTime(), processRestoreStats.GetPid(), timeEntry.GetName()})
		}
	}

	// Sort to get slowest time for stage
	for _, timingsForStage := range timingsByStage {
		sort.Slice(timingsForStage, func(i, j int) bool {
			return timingsForStage[i].time > timingsForStage[j].time
		})
	}

	// add slowest time to profiler for each stage
	for _, timingsForStage := range timingsByStage {
		if len(timingsForStage) == 0 {
			continue
		}
		slowestEntry := timingsForStage[0]
		pidStr := strconv.FormatUint(uint64(slowestEntry.pid), 10)
		profiling.AddTimingParallelComponent(restoreCtx, time.Duration(slowestEntry.time)*time.Microsecond, "SlowestPID="+pidStr+"="+slowestEntry.stage)
	}

	postRestoreTime := time.Duration(criuResp.Stats.GetPostRestoreTime()) * time.Microsecond
	profiling.AddTimingParallelComponent(ctx, postRestoreTime, "PostRestore")
}

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
	criuOpts.LogLevel = proto.Int32(config.Global.CRIU.LogLevel)
	criuOpts.LogToStderr = proto.Bool(false)
	criuOpts.GhostLimit = proto.Uint32(GHOST_FILE_MAX_SIZE)

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

	if err == nil {
		addRestoreProfiling(ctx, criuResp)
	}

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
			p, _ := os.FindProcess(int(resp.PID)) // always succeeds on linux
			status, err := p.Wait()
			if err != nil {
				log.Trace().Err(err).Msg("process Wait()")
			}
			exitCode <- status.ExitCode()
			close(exitCode)
		})
	}

	log.Info().Uint32("PID", resp.PID).Msg("CRIU restore complete")

	return code, err
}
