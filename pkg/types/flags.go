package types

// This file contains all the flags used in the cmd package.
// Should be imported by all the plugins as well, and modified as needed
// to avoid duplication of flags across different files.

type Flag struct {
	Full  string
	Short string
}

var (
	DirFlag    = Flag{"dir", "d"}
	PathFlag   = Flag{"path", "p"}
	StreamFlag = Flag{"stream", "s"}

	// CRIU
	LeaveRunningFlag   = Flag{"leave-running", ""}
	TcpEstablishedFlag = Flag{"tcp-established", ""}
	TcpCloseFlag       = Flag{"tcp-close", ""}

	// Root
	PortFlag      = Flag{"port", "P"}
	HostFlag      = Flag{"host", "H"}
	ConfigFlag    = Flag{"config", ""}
	ConfigDirFlag = Flag{"config-dir", ""}
	UseVSOCKFlag  = Flag{"use-vsock", ""}
)
