package types

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
	DirFlag        = Flag{"dir", "d"}
	PathFlag       = Flag{"path", "p"}
	StreamFlag     = Flag{"stream", "s"}
	WorkingDirFlag = Flag{"working-dir", "w"}
	JidFlag        = Flag{"jid", "j"}
	GpuEnabledFlag = Flag{"gpu-enabled", "g"}
	AttachFlag     = Flag{"attach", "a"}
	AllFlag        = Flag{"all", "a"}
	LogFlag        = Flag{"log", ""}

	// CRIU
	LeaveRunningFlag   = Flag{"leave-running", ""}
	TcpEstablishedFlag = Flag{"tcp-established", ""}
	TcpCloseFlag       = Flag{"tcp-close", ""}

	// Parent flags
	PortFlag       = Flag{"port", "P"}
	HostFlag       = Flag{"host", "H"}
	ConfigFlag     = Flag{"config", ""}
	ConfigDirFlag  = Flag{"config-dir", ""}
	UseVSOCKFlag   = Flag{"use-vsock", ""}
	MetricsASRFlag = Flag{"metrics-asr", ""}
)
