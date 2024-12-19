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
	DirFlag        = Flag{Full: "dir", Short: "d"}
	PathFlag       = Flag{Full: "path", Short: "p"}
	StreamFlag     = Flag{Full: "stream", Short: "s"}
	WorkingDirFlag = Flag{Full: "working-dir", Short: "w"}
	JidFlag        = Flag{Full: "jid", Short: "j"}
	GpuEnabledFlag = Flag{Full: "gpu-enabled", Short: "g"}
	AttachFlag     = Flag{Full: "attach", Short: "a"}
	AttachableFlag = Flag{Full: "attachable"}
	AllFlag        = Flag{Full: "all", Short: "a"}
	LogFlag        = Flag{Full: "log"}
	ExternalFlag   = Flag{Full: "external"}
	FileLocksFlag  = Flag{Full: "file-locks"}
	TypeFlag       = Flag{Full: "type", Short: "t"}

	// CRIU
	LeaveRunningFlag    = Flag{Full: "leave-running"}
	LeaveStoppedFlag    = Flag{Full: "leave-stopped"}
	TcpEstablishedFlag  = Flag{Full: "tcp-established"}
	TcpCloseFlag        = Flag{Full: "tcp-close"}
	TcpSkipInFlightFlag = Flag{Full: "skip-in-flight"}
	ShellJobFlag        = Flag{Full: "shell-job"}

	// Parent flags
	PortFlag       = Flag{Full: "port", Short: "P"}
	HostFlag       = Flag{Full: "host", Short: "H"}
	ConfigFlag     = Flag{Full: "config"}
	ConfigDirFlag  = Flag{Full: "config-dir"}
	UseVSOCKFlag   = Flag{Full: "use-vsock"}
	LocalDBFlag    = Flag{Full: "local-db"}
	ContextIdFlag  = Flag{Full: "cid"}
	MetricsASRFlag = Flag{Full: "metrics-asr"}
	ProfilingFlag  = Flag{Full: "profiling"}
)
