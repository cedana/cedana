package types

// Against better convention, types

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/shirou/gopsutil/v3/net"
	"github.com/shirou/gopsutil/v3/process"
)

type Job struct {
	Command string `json:"command"`
}

// CedanaState encapsulates a CRIU checkpoint and includes
// filesystem state for a full restore. Typically serialized and shot around
// over the wire.
type CedanaState struct {
	ClientInfo     ClientInfo     `json:"client_info" mapstructure:"client_info"`
	ProcessInfo    ProcessInfo    `json:"process_info" mapstructure:"process_info"`
	CheckpointType CheckpointType `json:"checkpoint_type" mapstructure:"checkpoint_type"`

	// either local or remote checkpoint path (url vs filesystem path)
	CheckpointPath string `json:"checkpoint_path" mapstructure:"checkpoint_path"`

	// Flags should be flicked on and stay consistent across
	// state updates
	Flags []Flag `json:"checkpoint_state" mapstructure:"checkpoint_state"`
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
	Command   string `json:"command" mapstructure:"command"`
	Heartbeat bool   `json:"heartbeat" mapstructure:"heartbeat"`
	// orchestrator passes back the latest cedanaState to the client which can be used to verify
	// the source prior to execution.
	// TODO NR - implement verification
	CedanaState CedanaState `json:"cedana_state" mapstructure:"cedana_state"`
}

type CheckpointType string

// Flag and FlagReason are used together when pushing up state.
// These should only encapsulate flags that an external service (like an orchestrator)
// can use for deciding what to do - especially in the case that the daemon reaches a point
// after which further actions are not possible (or shouldn't be possible).
type Flag string
type FlagReason string

const (
	CheckpointTypeNone    CheckpointType = "none"
	CheckpointTypeCRIU    CheckpointType = "criu"
	CheckpointTypePytorch CheckpointType = "pytorch"
)

const (
	CheckpointSuccess Flag = "CHECKPOINTED"
	CheckpointFailed  Flag = "CHECKPOINT_FAILED"
	RestoreSuccess    Flag = "RESTORED"
	RestoreFailed     Flag = "RESTORE_FAILED"

	// Job here refers to a process or container started and managed (C/R) by the daemon.
	JobStartupFailed Flag = "JOB_STARTUP_FAILED"
	JobKilled        Flag = "JOB_KILLED"
	JobIdle          Flag = "JOB_IDLE"
)
