package criu

import (
	"context"
	"path/filepath"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/channel"
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
	GHOST_FILE_MAX_SIZE = 200 * utils.MEBIBYTE
)

var Dump types.Dump = dump

// Returns a CRIU dump handler for the server
func dump(ctx context.Context, opts types.Opts, resp *daemon.DumpResp, req *daemon.DumpReq) (code func() <-chan int, err error) {
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
	criuOpts.LogLevel = proto.Int32(logLevel())
	criuOpts.GhostLimit = proto.Uint32(GHOST_FILE_MAX_SIZE)
	criuOpts.Pid = proto.Int32(int32(resp.GetState().GetPID()))
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

//////////////////////
// Helper functions //
//////////////////////

func logLevel() int32 {
	level := 1 // error statements
	if log.Logger.GetLevel() <= zerolog.TraceLevel {
		level = 3 // debug statements
	} else if log.Logger.GetLevel() <= zerolog.DebugLevel {
		level = 2 // warning statements
	}
	return int32(level)
}
