package handlers

import (
	"context"

	"github.com/rs/zerolog/log"
	"golang.org/x/sys/unix"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/channel"
	"github.com/cedana/cedana/pkg/types"
	"github.com/cedana/cedana/pkg/utils"
	clh "github.com/cedana/cedana/plugins/cloud-hypervisor/pkg/clh"
	"github.com/cedana/cedana/plugins/cloud-hypervisor/pkg/vm"
)

var Restore types.RestoreVM = restore

// Returns a VM dump handler for the server
func restore(ctx context.Context, opts types.Opts, resp *daemon.RestoreVMResp, req *daemon.RestoreVMReq) (code func() <-chan int, err error) {
	var netFds []int
	var netFdsInt64 []int64
	var snapshotter vm.Snapshotter

	snapshot := req.GetVMSnapshotPath()
	socketPath := req.GetVMSocketPath()
	restoredNetConfig := req.GetRestoredNetConfig()
	requestIDs := req.GetRequestIDs()

	snapshotter = &clh.CloudHypervisorVM{}

	if len(requestIDs) > 1 {
		return nil, status.Errorf(codes.Internal, "Restore vm task failed: more than one request ID provided, we do not support this yet")
	}

	requestID := requestIDs[0]
	var id string

	opts.FdStore.Range(func(key, value any) bool {
		id = key.(string) // Adjust the type to match the actual key type

		if requestID != id {
			return true
		}

		netFds = value.([]int) // Adjust the type to match the actual value type

		log.Logger.Info().Msgf("Request ID: %v, FDs: %v\n", requestID, netFds)

		return false
	})

	if id == "" {
		return nil, status.Errorf(codes.Internal, "Restore task failed: request ID not found in FD store")
	}

	opts.FdStore.Delete(id)

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

	defer func() {
		for _, fd := range netFds {
			unix.Close(fd)
		}
	}()

	return channel.Broadcaster(utils.WaitForPidCtx(opts.Lifetime, pid)), nil
}
