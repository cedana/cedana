package types

// Against better convention, types

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/cedana/cedana/api/services/task"
)

type Job struct {
	Command string `json:"command"`
}

// A process encapsulates either a process or a container
type ProcessState struct {
	PID              int32            `json:"pid"`
	Task             string           `json:"task"`
	ContainerRuntime ContainerRuntime `json:"container_runtime"`
	ContainerId      string           `json:"container_id"`
	StartedAt        time.Time        `json:"started_at"`
	ProcessInfo      ProcessInfo      `json:"process_info"`
	CheckpointPath   string           `json:"checkpoint_path"`
	CheckpointState  CheckpointState  `json:"checkpoint_state"`
	Flag             Flag             `json:"flag"`
}

func (ps *ProcessState) SerializeToFolder(dir string) error {
	serialized, err := json.MarshalIndent(ps, "", "  ")
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
	AttachedToHardwareAccel bool                  `json:"attached_to_hardware_accel" mapstructure:"attached_to_hardware_accel"`
	OpenFds                 []task.OpenFilesStat  `json:"open_fds" mapstructure:"open_fds"` // list of open FDs
	OpenWriteOnlyFilePaths  []string              `json:"open_write_only" mapstructure:"open_write_only"`
	OpenConnections         []task.ConnectionStat `json:"open_connections" mapstructure:"open_connections"` // open network connections
	MemoryPercent           float32               `json:"memory_percent" mapstructure:"memory_percent"`     // % of total RAM used
	IsRunning               bool                  `json:"is_running" mapstructure:"is_running"`
	Status                  string                `json:"status" mapstructure:"status"`
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
	// new job command to be executed
	UpdatedTask string `json:"updated_task" mapstructure:"updated_task"`
	RestorePath string `json:"restore_path" mapstructure:"restore_path"`
}

type Flag string
type CheckpointState string
type ContainerRuntime string

const (
	CheckpointSuccess CheckpointState = "CHECKPOINTED"
	CheckpointFailed  CheckpointState = "CHECKPOINT_FAILED"
	RestoreSuccess    CheckpointState = "RESTORED"
	RestoreFailed     CheckpointState = "RESTORE_FAILED"

	// Job here refers to a process or container started and managed (C/R) by the daemon.
	JobStartupFailed Flag = "JOB_STARTUP_FAILED"
	JobKilled        Flag = "JOB_KILLED"
	JobIdle          Flag = "JOB_IDLE"
	JobRunning       Flag = "JOB_RUNNING"
	JobPending       Flag = "JOB_PENDING"
	// setup is used by the orchestrator
	JobSetupFailed Flag = "JOB_SETUP_FAILED"
	JobDone        Flag = "JOB_DONE"

	// supported container runtimes
	ContainerRuntimeContainerd ContainerRuntime = "containerd"
	ContainerRuntimeRunc       ContainerRuntime = "runc"
)
