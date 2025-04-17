package handlers

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"syscall"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/types"
	clhclient "github.com/cedana/cedana/plugins/cloud-hypervisor/pkg/clh/client"
	utils "github.com/cedana/cedana/plugins/cloud-hypervisor/pkg/utils"
	vm "github.com/cedana/cedana/plugins/vm/pkg"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
)

var Run types.Run = run

// run runs a clh vm using cli + api
func run(ctx context.Context, opts types.Opts, resp *daemon.RunResp, req *daemon.RunReq) (exited chan int, err error) {
	details := req.GetDetails().GetVm()
	hypervisorConfig := details.GetHypervisorConfig()
	hypervisorConfigJson, err := protojson.Marshal(hypervisorConfig)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to marshal hypervisor config: %v", err)
	}

	clhConfig := &vm.HypervisorConfig{}

	json.Unmarshal(hypervisorConfigJson, &clhConfig)

	config := *clhclient.NewVmConfig(*clhclient.NewPayloadConfig())

	config.Payload.SetKernel(clhConfig.KernelPath)

	config.Platform = clhclient.NewPlatformConfig()
	config.Platform.SetNumPciSegments(2)
	if clhConfig.IOMMU {
		config.Platform.SetIommuSegments([]int32{0})
	}

	config.Memory = clhclient.NewMemoryConfig(int64((utils.MemUnit(clhConfig.MemorySize) * utils.MiB)))

	// Force shared mem to be false
	config.Memory.Shared = func(b bool) *bool { return &b }(false)

	config.Cpus = clhclient.NewCpusConfig(int32(clhConfig.NumVCPUsF), int32(clhConfig.DefaultMaxVCPUs))

	rawDisk := clhclient.NewDiskConfig()
	rawDisk.SetPath(clhConfig.RawImagePath)

	imgDisk := clhclient.NewDiskConfig()
	imgDisk.SetPath(clhConfig.ImagePath)

	config.Disks = append(config.Disks, *rawDisk, *imgDisk)

	args := []string{"--api-socket", "/tmp/clh.sock"}
	args = append(args, "-vv")
	args = append(args, "--log-file", "/tmp/clh.log")
	args = append(args, "--seccomp", "false")

	// TODO don't hardcode this
	cmd := exec.Command("/usr/local/bin/cloud-hypervisor", args...)

	cmd.Env = os.Environ()
	cmd.Env = append(cmd.Env, "RUST_BACKTRACE=full")
	cmd.Stderr = cmd.Stdout

	cmd.SysProcAttr = &syscall.SysProcAttr{
		Credential: &syscall.Credential{Uid: uint32(0), Gid: uint32(0)},
		// Pdeathsig: syscall.SIGKILL, // kill even if server dies suddenly
		// XXX: Above is commented out because if we try to restore a managed job,
		// one that was started by the daemon,
		// using a dump path (directly w/ restore -p <path>), instead of using job
		// restore, the restored process dies immediately.
	}

	err = cmd.Start()
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to start clh vm: %v", err)
	}

	return exited, nil
}
