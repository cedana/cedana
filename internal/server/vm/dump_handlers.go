package criu

import (
	"context"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	clhclient "github.com/cedana/cedana/pkg/clh"
	"github.com/cedana/cedana/pkg/types"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	CRIU_DUMP_LOG_FILE  = "criu-dump.log"
	GHOST_FILE_MAX_SIZE = 10000000 // 10MB
)

var CRIU_LOG_VERBOSITY_LEVEL int32 = 1 // errors only

func init() {
	if log.Logger.GetLevel() <= zerolog.TraceLevel {
		CRIU_LOG_VERBOSITY_LEVEL = 3 // debug statements
	} else if log.Logger.GetLevel() <= zerolog.DebugLevel {
		CRIU_LOG_VERBOSITY_LEVEL = 2 // warning statements
	}
}

var Dump types.DumpVM = dump

// Returns a CRIU dump handler for the server
func dump(ctx context.Context, opts types.Opts, resp *daemon.DumpVMResp, req *daemon.DumpVMReq) (exited chan int, err error) {

	clhClient := clhclient.NewClient()
	err := s.vmSnapshotter.Pause(req.VMSocketPath)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Checkpoint task failed: %v", err)
	}

	var resumeErr error
	defer func() {
		if err := s.vmSnapshotter.Resume(args.VMSocketPath); err != nil {
			resumeErr = status.Errorf(codes.Internal, "Checkpoint task failed during resume: %v", err)
		}
	}()

	err = s.vmSnapshotter.Snapshot(args.Dir, args.VMSocketPath, vm)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Checkpoint task failed during snapshot: %v", err)
	}

	if resumeErr != nil {
		return nil, resumeErr
	}
	return &daemon.DumpVMResp{TarDumpDir: args.Dir}, nil
}
