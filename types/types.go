package types

// Against better convention, types

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/shirou/gopsutil/v3/net"
	"github.com/shirou/gopsutil/v3/process"
)

// CedanaState encapsulates a CRIU checkpoint and includes
// filesystem state for a full restore. Typically serialized and shot around
// over the wire.
type CedanaState struct {
	ClientInfo     ClientInfo     `json:"client_info" mapstructure:"client_info"`
	ProcessInfo    ProcessInfo    `json:"process_info" mapstructure:"process_info"`
	CheckpointType CheckpointType `json:"checkpoint_type" mapstructure:"checkpoint_type"`
	// either local or remote checkpoint path (url vs filesystem path)
	CheckpointPath string `json:"checkpoint_path" mapstructure:"checkpoint_path"`
	// process state at time of checkpoint
	CheckpointState CheckpointState `json:"checkpoint_state" mapstructure:"checkpoint_state"`
}

func (cs *CedanaState) SerializeToFolder(dir string) error {
	serialized, err := json.MarshalIndent(cs, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(dir, "checkpoint_state.json")
	file, err := os.Create(path)
	if err != nil {
		return err
	}

	defer file.Close()
	_, err = file.Write(serialized)
	return err
}

type Logs struct {
	Stdout string `mapstructure:"stdout"`
	Stderr string `mapstructure:"stderr"`
}

type ProcessInfo struct {
	PID                     int32                   `json:"pid" mapstructure:"pid"`
	AttachedToHardwareAccel bool                    `json:"attached_to_hardware_accel" mapstructure:"attached_to_hardware_accel"`
	OpenFds                 []process.OpenFilesStat `json:"open_fds" mapstructure:"open_fds"` // list of open FDs
	OpenWriteOnlyFilePaths  []string                `json:"open_write_only" mapstructure:"open_write_only"`
	OpenConnections         []net.ConnectionStat    `json:"open_connections" mapstructure:"open_connections"` // open network connections
	MemoryPercent           float32                 `json:"memory_percent" mapstructure:"memory_percent"`     // % of total RAM used
	IsRunning               bool                    `json:"is_running" mapstructure:"is_running"`
	Status                  string                  `json:"status" mapstructure:"status"`
}

type ClientInfo struct {
	Id              string `json:"id" mapstructure:"id"`
	Hostname        string `json:"hostname" mapstructure:"hostname"`
	Platform        string `json:"platform" mapstructure:"platform"`
	OS              string `json:"os" mapstructure:"os"`
	Uptime          uint64 `json:"uptime" mapstructure:"uptime"`
	RemainingMemory uint64 `json:"remaining_memory" mapstructure:"remaining_memory"`
}

type GPUInfo struct {
	Count            int       `json:"count" mapstructure:"count"`
	UtilizationRates []float64 `json:"utilization_rates" mapstructure:"utilization_rates"`
	PowerUsage       uint64    `json:"power_usage" mapstructure:"power_usage"`
}

type ServerCommand struct {
	Command     string      `json:"command" mapstructure:"command"`
	Heartbeat   bool        `json:"heartbeat" mapstructure:"heartbeat"`
	CedanaState CedanaState `json:"cedana_state" mapstructure:"cedana_state"`
}

type CheckpointType string
type CheckpointState string

const (
	CheckpointTypeNone    CheckpointType = "none"
	CheckpointTypeCRIU    CheckpointType = "criu"
	CheckpointTypePytorch CheckpointType = "pytorch"
)

const (
	CheckpointSuccess CheckpointState = "CHECKPOINTED"
	CheckpointFailed  CheckpointState = "CHECKPOINT_FAILED"
	RestoreSuccess    CheckpointState = "RESTORED"
	RestoreFailed     CheckpointState = "RESTORE_FAILED"
)
