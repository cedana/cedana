package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"syscall"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/types"
	clh "github.com/cedana/cedana/plugins/cloud-hypervisor/pkg/clh"
	clhclient "github.com/cedana/cedana/plugins/cloud-hypervisor/pkg/clh/client"
	utils "github.com/cedana/cedana/plugins/cloud-hypervisor/pkg/utils"
	vm "github.com/cedana/cedana/plugins/vm/pkg"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
)

var Run types.Run = run

const API_SOCK = "/tmp/clh.sock"

type clhClientApi struct {
	ApiInternal *clhclient.DefaultAPIService
}

// run runs a clh vm using cli + api
func run(ctx context.Context, opts types.Opts, resp *daemon.RunResp, req *daemon.RunReq) (exited chan int, err error) {
	details := req.GetDetails().GetVm()
	hypervisorConfig := details.GetHypervisorConfig()
	hypervisorConfigJson, err := protojson.Marshal(hypervisorConfig)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to marshal hypervisor config: %v", err)
	}

	clhConfig := &vm.HypervisorConfig{}
	vm := &clh.CloudHypervisorVM{}

	json.Unmarshal(hypervisorConfigJson, &clhConfig)

	vm.Config = *clhclient.NewVmConfig(*clhclient.NewPayloadConfig())

	vm.Config.Payload.SetKernel(clhConfig.KernelPath)

	vm.Config.Platform = clhclient.NewPlatformConfig()
	vm.Config.Platform.SetNumPciSegments(2)
	if clhConfig.IOMMU {
		vm.Config.Platform.SetIommuSegments([]int32{0})
	}

	vm.Config.Memory = clhclient.NewMemoryConfig(int64((utils.MemUnit(clhConfig.MemorySize) * utils.MiB)))

	// Force shared mem to be false
	vm.Config.Memory.Shared = func(b bool) *bool { return &b }(false)

	vm.Config.Cpus = clhclient.NewCpusConfig(int32(clhConfig.NumVCPUsF), int32(clhConfig.DefaultMaxVCPUs))

	rawDisk := clhclient.NewDiskConfig(clhConfig.RawImagePath)

	imgDisk := clhclient.NewDiskConfig(clhConfig.ImagePath)

	vm.Config.Disks = append(vm.Config.Disks, *rawDisk, *imgDisk)

	args := []string{"--api-socket", API_SOCK}
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

	vm.Pid = cmd.Process.Pid

	cfg := clhclient.NewConfiguration()
	cfg.HTTPClient = &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, path string) (net.Conn, error) {
				addr, err := net.ResolveUnixAddr("unix", API_SOCK)
				if err != nil {
					return nil, err
				}

				return net.DialUnix("unix", nil, addr)
			},
		},
	}

	apiClient := &clhClientApi{
		ApiInternal: clhclient.NewAPIClient(cfg).DefaultAPI,
	}

	vm.APIClient = apiClient.ApiInternal

	if err := vm.WaitVMM(10); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to wait for clh vm: %v", err)
	}

	_, err = os.Stat(API_SOCK)
	if err != nil && os.IsNotExist(err) {
		return nil, status.Errorf(codes.Internal, "failed to find API socket: %v", err)
	}

	_, err = vm.APIClient.CreateVM(ctx).VmConfig(vm.Config).Execute()
	if err != nil {
		return nil, utils.OpenAPIClientError(err)
	}

	info, err := vm.VMInfo()
	if err != nil {
		return nil, err
	}

	if info.State != "Created" {
		return nil, fmt.Errorf("VM state is not 'Created' after 'CreateVM'")
	}

	_, err = vm.APIClient.BootVM(ctx).Execute()
	if err != nil {
		log.Info().Msgf("VM Config: %v", vm.Config)
		return nil, utils.OpenAPIClientError(err)
	}

	info, err = vm.VMInfo()
	if err != nil {
		return nil, err
	}

	if info.State != "Running" {
		return nil, fmt.Errorf("VM state is not 'Running' after 'BootVM'")
	}

	return exited, nil
}
