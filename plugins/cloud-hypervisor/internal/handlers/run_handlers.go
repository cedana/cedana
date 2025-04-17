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

	clhConfig := &HypervisorConfig{}

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

// HypervisorConfig is the hypervisor configuration.
type HypervisorConfig struct {
	Groups       []uint32
	KernelPath   string
	ImagePath    string
	RawImagePath string

	InitrdPath          string
	RootfsType          string
	FirmwarePath        string
	FirmwareVolumePath  string
	MachineAccelerators string
	CPUFeatures         string
	HypervisorPath      string
	HypervisorCtlPath   string
	JailerPath          string

	BlockDeviceDriver string

	HypervisorMachineType string

	MemoryPath string

	DevicesStatePath     string
	EntropySource        string
	SharedFS             string
	SharedPath           string
	VirtioFSDaemon       string
	VirtioFSCache        string
	FileBackedMemRootDir string

	VhostUserStorePath string

	VhostUserDeviceReconnect uint32
	GuestMemoryDumpPath      string
	GuestHookPath            string

	VMid                   string
	VMStorePath            string
	RunStorePath           string
	SELinuxProcessLabel    string
	HypervisorPathList     []string
	HypervisorCtlPathList  []string
	JailerPathList         []string
	EntropySourceList      []string
	VirtioFSDaemonList     []string
	VirtioFSExtraArgs      []string
	EnableAnnotations      []string
	FileBackedMemRootList  []string
	PFlash                 []string
	VhostUserStorePathList []string
	SeccompSandbox         string
	BlockDeviceAIO         string
	RemoteHypervisorSocket string
	SandboxName            string
	SandboxNamespace       string
	User                   string

	SnpCertsPath string
	SGXEPCSize   int64

	DiskRateLimiterBwMaxRate int64

	DiskRateLimiterBwOneTimeBurst int64

	DiskRateLimiterOpsMaxRate int64

	DiskRateLimiterOpsOneTimeBurst int64
	RxRateLimiterMaxRate           uint64

	TxRateLimiterMaxRate uint64

	NetRateLimiterBwMaxRate int64

	NetRateLimiterBwOneTimeBurst int64

	NetRateLimiterOpsMaxRate int64

	NetRateLimiterOpsOneTimeBurst int64

	MemOffset               uint64
	VFIODevices             []DeviceInfo
	VhostUserBlkDevices     []DeviceInfo
	HotPlugVFIO             PCIePort
	ColdPlugVFIO            PCIePort
	PCIeRootPort            uint32
	PCIeSwitchPort          uint32
	NumVCPUsF               float32
	DefaultMaxVCPUs         uint32
	MemorySize              uint32
	DefaultMaxMemorySize    uint64
	DefaultBridges          uint32
	Msize9p                 uint32
	MemSlots                uint32
	VirtioFSCacheSize       uint32
	VirtioFSQueueSize       uint32
	Uid                     uint32
	Gid                     uint32
	RemoteHypervisorTimeout uint32
	BlockDeviceCacheSet     bool

	BlockDeviceCacheDirect bool

	BlockDeviceCacheNoflush bool
	DisableBlockDeviceUse   bool

	EnableIOThreads bool

	Debug         bool
	MemPrealloc   bool
	HugePages     bool
	VirtioMem     bool
	IOMMU         bool
	IOMMUPlatform bool

	DisableNestingChecks bool

	DisableImageNvdimm bool

	GuestMemoryDumpPaging bool
	ConfidentialGuest     bool

	SevSnpGuest bool

	BootToBeTemplate bool

	BootFromTemplate bool

	DisableVhostNet bool

	EnableVhostUserStore bool

	GuestSwap bool

	Rootless bool

	DisableSeccomp bool

	DisableSeLinux bool

	DisableGuestSeLinux bool

	LegacySerial bool

	QgsPort uint32
}

type DeviceInfo struct {
	// example block device: DriverOptions["block-driver"]="virtio-blk"
	DriverOptions map[string]string

	HostPath string

	// Type of device: c, b, u or p
	// c , u - character(unbuffered)
	// p - FIFO
	// b - block(buffered) special file
	// More info in mknod(1).
	DevType string

	ID string

	Major int64
	Minor int64

	FileMode os.FileMode

	UID uint32

	GID uint32

	Pmem bool

	ReadOnly bool

	Port PCIePort
}

type PCIePort string
