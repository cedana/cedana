package types

// This file contains all the flags used in the cmd package.
// Should be imported by all the plugins as well, and modified as needed
// to avoid duplication of flags across different files.

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

	// Runc
	RootFlag   = Flag{"root", "r"}
	BundleFlag = Flag{"bundle", "b"}

	// Parent flags
	PortFlag      = Flag{"port", "P"}
	HostFlag      = Flag{"host", "H"}
	ConfigFlag    = Flag{"config", ""}
	ConfigDirFlag = Flag{"config-dir", ""}
	UseVSOCKFlag  = Flag{"use-vsock", ""}
)
