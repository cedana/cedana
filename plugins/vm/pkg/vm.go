package vm

import "os"

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
