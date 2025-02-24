package vm

import (
	"context"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/clh"
	"github.com/cedana/cedana/pkg/types"
	"github.com/cedana/cedana/pkg/utils"
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

type VMSnapshotter interface {
	Snapshot(destinationURL, vmSocketPath, vmID string) error
	Restore(snapshotPath, vmSocketPath string, netConfigs []*daemon.RestoredNetConfig) error
	Pause(vmSocketPath string) error
	Resume(vmSocketPath string) error
	GetPID(vmSocketPath string) (uint32, error)
}

// Returns a VM dump handler for the server
func dump(ctx context.Context, opts types.Opts, resp *daemon.DumpVMResp, req *daemon.DumpVMReq) (exited chan int, err error) {
	var snapshotter VMSnapshotter

	switch req.Type {
	case "cloud-hypervisor":
		snapshotter = &clh.CloudHypervisorVM{}
	default:
		return nil, status.Errorf(codes.InvalidArgument, "Unknown VM type: %s", req.Type)
	}

	err = snapshotter.Pause(req.VMSocketPath)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Checkpoint task failed: %v", err)
	}

	var resumeErr error
	defer func() {
		if err := snapshotter.Resume(req.VMSocketPath); err != nil {
			resumeErr = status.Errorf(codes.Internal, "Checkpoint task failed during resume: %v", err)
		}
	}()

	err = snapshotter.Snapshot(req.Dir, req.VMSocketPath, req.VmName)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Checkpoint task failed during snapshot: %v", err)
	}

	if resumeErr != nil {
		return nil, resumeErr
	}

	pid, err := snapshotter.GetPID(req.VMSocketPath)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Failed to get PID: %v", err)
	}

	return utils.WaitForPid(pid), nil
}
