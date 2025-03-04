package handlers

import (
	"context"

	"github.com/rs/zerolog/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/types"
	"github.com/cedana/cedana/pkg/utils"
	clh "github.com/cedana/cedana/plugins/cloud-hypervisor/pkg/clh"
)

var Restore types.RestoreVM = restore

// Returns a VM dump handler for the server
func restore(ctx context.Context, opts types.Opts, resp *daemon.RestoreVMResp, req *daemon.RestoreVMReq) (exited chan int, err error) {
	var netFds []int
	var netFdsInt64 []int64
	var snapshotter VMSnapshotter

	snapshot := req.GetVMSnapshotPath()
	socketPath := req.GetVMSocketPath()
	restoredNetConfig := req.GetRestoredNetConfig()

	snapshotter = &clh.CloudHypervisorVM{}

	opts.FdStore.Range(func(key, value any) bool {
		requestID := key.(string) // Adjust the type to match the actual key type
		netFds = value.([]int)    // Adjust the type to match the actual value type

		log.Logger.Info().Msgf("Request ID: %v, FDs: %v\n", requestID, netFds)

		return true
	})

	netFdsInt64 = make([]int64, len(netFds))
	for i, fd := range netFds {
		netFdsInt64[i] = int64(fd)
	}

	restoredNetConfig[0].Fds = netFdsInt64
	err = snapshotter.Restore(snapshot, socketPath, restoredNetConfig)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Restore task failed during vmSnapshotter Restore: %v", err)
	}

	err = snapshotter.Resume(socketPath)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Restore task failed during vmSnapshotter Restore: %v", err)
	}

	pid, err := snapshotter.GetPID(socketPath)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Failed to get PID: %v", err)
	}

	return utils.WaitForPid(pid), nil
}
