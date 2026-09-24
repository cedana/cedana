package criu

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	criu_proto "buf.build/gen/go/cedana/criu/protocolbuffers/go/criu"
	"github.com/cedana/cedana/pkg/channel"
	"github.com/cedana/cedana/pkg/config"
	criu_client "github.com/cedana/cedana/pkg/criu"
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

const (
	CRIU_DUMP_LOG_FILE  = "criu-dump.log"
	GHOST_FILE_MAX_SIZE = 800 * utils.MEBIBYTE
)

// Returns a CRIU dump handler for the server
func Dump(ctx context.Context, opts types.Opts, resp *daemon.DumpResp, req *daemon.DumpReq) (code func() <-chan int, err error) {
	if req.GetCriu() == nil {
		return nil, status.Error(codes.InvalidArgument, "criu options is nil")
	}

	version, err := opts.CRIU.GetCriuVersion(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to get CRIU version: %v", err)
	}

	log := log.With().Str("plugin", "CRIU").Int("version", version).Str("operation", "dump").Uint32("PID", resp.State.PID).Logger()

	criuOpts := req.GetCriu()

	// Set CRIU server
	criuOpts.LogFile = proto.String(CRIU_DUMP_LOG_FILE)
	criuOpts.LogLevel = proto.Int32(config.Global.CRIU.LogLevel)
	criuOpts.LogToStderr = proto.Bool(false)
	criuOpts.GhostLimit = proto.Uint32(GHOST_FILE_MAX_SIZE)
	criuOpts.Pid = proto.Int32(int32(resp.GetState().GetPID()))

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

	if rounds := predumpRounds(); rounds > 0 {
		if err := preDump(ctx, opts, criuOpts, rounds, &log); err != nil {
			log.Warn().Err(err).Msg("pre-dump failed; dumping without a parent image")
			criuOpts.TrackMem = nil
			criuOpts.ParentImg = nil
		}
	}

	log.Info().Msg("CRIU dump starting")
	log.Debug().Interface("opts", criuOpts).Msg("CRIU dump options")

	ctx, end := profiling.StartTimingCategory(ctx, "criu", opts.CRIU.Dump)

	_, err = opts.CRIU.Dump(ctx, criuOpts, opts.CRIUCallback)

	end()

	logging.FromFile(
		log.WithContext(ctx),
		filepath.Join(criuOpts.GetImagesDir(), CRIU_DUMP_LOG_FILE),
		zerolog.TraceLevel,
	)

	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed CRIU dump: %v", err)
	}

	utils.ChownAll(criuOpts.GetImagesDir(), int(uids[0]), int(gids[0]))

	log.Info().Msg("CRIU dump complete")

	return channel.Broadcaster(utils.WaitForPidCtx(opts.Lifetime, resp.State.PID)), nil
}

// predumpRounds reads how many pre-dump passes to run before the real dump.
func predumpRounds() int {
	return max(config.Global.CRIU.PredumpRounds, 0)
}

func preDump(ctx context.Context, opts types.Opts, criuOpts *criu_proto.CriuOpts, rounds int, log *zerolog.Logger) error {
	parent := ""
	for i := range rounds {
		name := fmt.Sprintf("parent-%d", i)
		dir := filepath.Join(criuOpts.GetImagesDir(), name)
		if err := os.Mkdir(dir, 0o755); err != nil {
			return fmt.Errorf("failed to create pre-dump dir: %w", err)
		}
		f, err := os.Open(dir)
		if err != nil {
			return fmt.Errorf("failed to open pre-dump dir: %w", err)
		}

		pre := proto.Clone(criuOpts).(*criu_proto.CriuOpts)
		pre.ImagesDir = proto.String(dir)
		pre.ImagesDirFd = proto.Int32(int32(f.Fd()))
		pre.TrackMem = proto.Bool(true)
		pre.LeaveRunning = proto.Bool(true)
		if parent != "" {
			pre.ParentImg = proto.String(filepath.Join("..", parent))
		}

		start := time.Now()
		err = opts.CRIU.PreDump(ctx, pre, &criu_client.NotifyCallback{Name: "pre-dump"})
		f.Close()
		if err != nil {
			return fmt.Errorf("pre-dump pass %d failed: %w", i, err)
		}
		log.Info().Int("pass", i).Dur("took", time.Since(start)).Msg("CRIU pre-dump pass complete")
		parent = name
	}

	criuOpts.TrackMem = proto.Bool(true)
	criuOpts.ParentImg = proto.String(parent)
	return nil
}
