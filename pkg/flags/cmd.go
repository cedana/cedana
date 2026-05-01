package flags

// This file contains all the flags used in the cmd package.
// Should be consulted when adding new flags in a plugin
// to avoid conflicts. May also be imported by a plugin
// if it wants to access the value of a parent cmd flag.

// NOTE: Do not add plugin flags here. Plugin flags should be
// defined in the plugin's own types package.

type Flag struct {
	Full  string
	Short string
}

var (
	DirFlag         = Flag{Full: "dir", Short: "d"}
	NameFlag        = Flag{Full: "name"}
	PathFlag        = Flag{Full: "path", Short: "p"}
	PidFileFlag     = Flag{Full: "pid-file"}
	NoServerFlag    = Flag{Full: "no-server"}
	StreamsFlag     = Flag{Full: "streams"}
	WorkingDirFlag  = Flag{Full: "working-dir", Short: "w"}
	JidFlag         = Flag{Full: "jid", Short: "j"}
	GpuEnabledFlag  = Flag{Full: "gpu-enabled", Short: "g"}
	GpuTracingFlag  = Flag{Full: "gpu-tracing"}
	GpuIdFlag       = Flag{Full: "gpu-id"}
	AttachFlag      = Flag{Full: "attach", Short: "a"}
	AttachableFlag  = Flag{Full: "attachable"}
	AllFlag         = Flag{Full: "all", Short: "a"}
	OutFlag         = Flag{Full: "out", Short: "o"}
	ExternalFlag    = Flag{Full: "external"}
	FileLocksFlag   = Flag{Full: "file-locks"}
	TypeFlag        = Flag{Full: "type", Short: "t"}
	FullFlag        = Flag{Full: "full"}
	ErrorsFlag      = Flag{Full: "errors"}
	CompressionFlag = Flag{Full: "compression"}
	AsRootFlag      = Flag{Full: "as-root"}
	UpcomingFlag    = Flag{Full: "upcoming"}
	TreeFlag        = Flag{Full: "tree", Short: "t"}
	InspectFlag     = Flag{Full: "inspect", Short: "i"}

	// CRIU
	CriuOptsFlag        = Flag{Full: "criu-opts"}
	LeaveRunningFlag    = Flag{Full: "leave-running"}
	LeaveStoppedFlag    = Flag{Full: "leave-stopped"}
	TcpEstablishedFlag  = Flag{Full: "tcp-established"}
	TcpCloseFlag        = Flag{Full: "tcp-close"}
	TcpSkipInFlightFlag = Flag{Full: "skip-in-flight"}
	ShellJobFlag        = Flag{Full: "shell-job"}
	LinkRemapFlag       = Flag{Full: "link-remap"}

	// Parent flags
	AddressFlag   = Flag{Full: "address"}
	ProtocolFlag  = Flag{Full: "protocol"}
	InitConfig    = Flag{Full: "init-config"}
	MergeConfig   = Flag{Full: "merge-config"}
	ConfigFlag    = Flag{Full: "config"}
	ConfigDirFlag = Flag{Full: "config-dir"}
	DBFlag        = Flag{Full: "db"}
	ProfilingFlag = Flag{Full: "profiling"}

	// Restore notifications
	NotifyFlag              = Flag{Full: "notify"}
	EventFlag               = Flag{Full: "event"}
	RestoreIDFlag           = Flag{Full: "restore-id"}
	NotificationNameFlag    = Flag{Full: "notification-name"}
	RouterFlag              = Flag{Full: "router"}
	RabbitMQURLFlag         = Flag{Full: "rabbitmq-url"}
	ClusterIDFlag           = Flag{Full: "cluster-id"}
	WorkloadTypeFlag        = Flag{Full: "workload-type"}
	CheckpointIDFlag        = Flag{Full: "checkpoint-id"}
	CheckpointActionIDFlag  = Flag{Full: "checkpoint-action-id"}
	ActionIDFlag            = Flag{Full: "action-id"}
	ActionScopeFlag         = Flag{Full: "action-scope"}
	PathIDFlag              = Flag{Full: "path-id"}
	RestorePathFlag         = Flag{Full: "restore-path"}
	StorageProviderFlag     = Flag{Full: "storage-provider"}
	ErrorMessageFlag        = Flag{Full: "error-message"}
	MetadataFlag            = Flag{Full: "metadata"}
	RequestMetadataFlag     = Flag{Full: "request-metadata"}
	RuntimeMetadataFlag     = Flag{Full: "runtime-metadata"}
	ProfilingPathFlag       = Flag{Full: "profiling-path"}
	ProfilingUploadPathFlag = Flag{Full: "profiling-upload-path"}
	UploadProfilingFlag     = Flag{Full: "upload-profiling"}
)
