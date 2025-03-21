package vm

import "buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"

type Snapshotter interface {
	Snapshot(destinationURL, vmSocketPath, vmID string) error
	Restore(snapshotPath, vmSocketPath string, netConfigs []*daemon.RestoredNetConfig) error
	Pause(vmSocketPath string) error
	Resume(vmSocketPath string) error
	GetPID(vmSocketPath string) (uint32, error)
}
