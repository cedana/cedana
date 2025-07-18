package handlers

import (
	"context"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/channel"
	"github.com/cedana/cedana/pkg/types"
	"github.com/cedana/cedana/pkg/utils"
	clh "github.com/cedana/cedana/plugins/cloud-hypervisor/pkg/clh"
	"github.com/cedana/cedana/plugins/cloud-hypervisor/pkg/vm"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var Dump types.DumpVM = dump

// Returns a VM dump handler for the server
func dump(ctx context.Context, opts types.Opts, resp *daemon.DumpVMResp, req *daemon.DumpVMReq) (code func() <-chan int, err error) {
	var snapshotter vm.Snapshotter

	snapshotter = &clh.CloudHypervisorVM{}

	err = snapshotter.Pause(req.Details.Kata.VmSocket)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Checkpoint task failed: %v", err)
	}

	var resumeErr error
	defer func() {
		if err := snapshotter.Resume(req.Details.Kata.VmSocket); err != nil {
			resumeErr = status.Errorf(codes.Internal, "Checkpoint task failed during resume: %v", err)
		}
	}()

	err = snapshotter.Snapshot(req.Dir, req.Details.Kata.VmSocket, req.Details.Kata.VmID)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Checkpoint task failed during snapshot: %v", err)
	}

	if resumeErr != nil {
		return nil, resumeErr
	}

	pid, err := snapshotter.GetPID(req.Details.Kata.VmSocket)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Failed to get PID: %v", err)
	}

	return channel.Broadcaster(utils.WaitForPidCtx(opts.Lifetime, pid)), nil
}
