package filewatcher

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/plugins/containerd"
	"github.com/cedana/cedana/pkg/client"
	"github.com/cedana/cedana/pkg/config"
	"github.com/rs/zerolog"
)

// FileWatcher polls for checkpoint trigger files in containers and triggers checkpoint/restore operations
type FileWatcher struct {
	client       *client.Client
	pollInterval time.Duration
	triggers     []config.FileTrigger
	log          zerolog.Logger
}

// New creates a new file watcher
func New(client *client.Client, cfg config.FileWatching, log zerolog.Logger) (*FileWatcher, error) {
	if !cfg.Enabled {
		return nil, fmt.Errorf("file watching is disabled")
	}

	pollInterval, err := time.ParseDuration(cfg.PollInterval)
	if err != nil {
		return nil, fmt.Errorf("invalid poll_interval %q: %w", cfg.PollInterval, err)
	}

	if len(cfg.Triggers) == 0 {
		return nil, fmt.Errorf("no file triggers configured")
	}

	return &FileWatcher{
		client:       client,
		pollInterval: pollInterval,
		triggers:     cfg.Triggers,
		log:          log,
	}, nil
}

// Start begins watching for trigger files
func (fw *FileWatcher) Start(ctx context.Context) error {
	fw.log.Info().
		Dur("poll_interval", fw.pollInterval).
		Int("triggers", len(fw.triggers)).
		Msg("starting file watcher")

	ticker := time.NewTicker(fw.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := fw.poll(ctx); err != nil {
				fw.log.Error().Err(err).Msg("poll error")
			}
		}
	}
}

func (fw *FileWatcher) poll(ctx context.Context) error {
	// Query all running containers
	queryResp, err := fw.client.Query(ctx, &daemon.QueryReq{
		Type: "containerd",
		Containerd: &containerd.QueryReq{
			IDs: []string{}, // Empty = all containers
		},
	})
	if err != nil {
		return fmt.Errorf("failed to query containers: %w", err)
	}

	if queryResp.Containerd == nil || len(queryResp.Containerd.Containers) == 0 {
		return nil
	}

	// Check each container for trigger files
	for _, container := range queryResp.Containerd.Containers {
		for _, trigger := range fw.triggers {
			if err := fw.checkTrigger(ctx, container, trigger); err != nil {
				fw.log.Error().
					Err(err).
					Str("container_id", container.GetID()).
					Str("trigger_path", trigger.Path).
					Msg("failed to check trigger")
			}
		}
	}

	return nil
}

func (fw *FileWatcher) checkTrigger(ctx context.Context, container *containerd.Containerd, trigger config.FileTrigger) error {
	// Construct path to file in container's rootfs
	// For containerd, the rootfs is at <bundle>/rootfs
	if container.GetRunc() == nil {
		return fmt.Errorf("container has no runc runtime info")
	}

	rootfsPath := filepath.Join(container.GetRunc().GetBundle(), "rootfs")
	triggerPath := filepath.Join(rootfsPath, trigger.Path)

	// Check if file exists
	if _, err := os.Stat(triggerPath); os.IsNotExist(err) {
		return nil // File doesn't exist, nothing to do
	} else if err != nil {
		return fmt.Errorf("failed to stat trigger file: %w", err)
	}

	// File exists! Trigger the action
	fw.log.Info().
		Str("container_id", container.ID).
		Str("trigger_path", trigger.Path).
		Str("action", trigger.Action).
		Msg("trigger file detected")

	switch trigger.Action {
	case "checkpoint":
		return fw.handleCheckpoint(ctx, container, trigger, triggerPath)
	case "restore":
		return fw.handleRestore(ctx, container, trigger, triggerPath)
	default:
		return fmt.Errorf("unknown trigger action: %s", trigger.Action)
	}
}

func (fw *FileWatcher) handleCheckpoint(ctx context.Context, container *containerd.Containerd, trigger config.FileTrigger, triggerPath string) error {
	// Query container state to get PID
	queryResp, err := fw.client.Query(ctx, &daemon.QueryReq{
		Type: "containerd",
		Containerd: &containerd.QueryReq{
			IDs: []string{container.ID},
		},
	})
	if err != nil {
		return fmt.Errorf("failed to query container: %w", err)
	}

	if len(queryResp.States) == 0 {
		return fmt.Errorf("container has no state")
	}

	containerPID := int(queryResp.States[0].PID)

	// Build dump request
	dumpReq := &daemon.DumpReq{
		Name: fmt.Sprintf("file-trigger-%s-%d", container.ID[:12], time.Now().Unix()),
		Type: "containerd",
		Dir:  config.Global.Checkpoint.Dir,
		Details: &daemon.Details{
			Containerd: container,
		},
	}

	// Perform checkpoint: Freeze → Dump → Unfreeze
	fw.log.Info().Str("container_id", container.ID).Msg("freezing container")
	if _, _, err := fw.client.Freeze(ctx, dumpReq); err != nil {
		fw.sendSignal(containerPID, trigger.OnFailure, "checkpoint freeze failed")
		return fmt.Errorf("freeze failed: %w", err)
	}

	fw.log.Info().Str("container_id", container.ID).Msg("dumping container")
	dumpResp, _, err := fw.client.Dump(ctx, dumpReq)
	if err != nil {
		fw.client.Unfreeze(ctx, dumpReq) // Try to unfreeze
		fw.sendSignal(containerPID, trigger.OnFailure, "checkpoint dump failed")
		return fmt.Errorf("dump failed: %w", err)
	}

	fw.log.Info().Str("container_id", container.ID).Msg("unfreezing container")
	if _, _, err := fw.client.Unfreeze(ctx, dumpReq); err != nil {
		fw.sendSignal(containerPID, trigger.OnFailure, "checkpoint unfreeze failed")
		return fmt.Errorf("unfreeze failed: %w", err)
	}

	fw.log.Info().
		Str("container_id", container.ID).
		Str("checkpoint_path", dumpResp.Paths[0]).
		Msg("checkpoint completed successfully")

	// Remove trigger file
	if err := os.Remove(triggerPath); err != nil {
		fw.log.Warn().Err(err).Str("path", triggerPath).Msg("failed to remove trigger file")
	}

	// Send success signal
	fw.sendSignal(containerPID, trigger.OnSuccess, "checkpoint complete")

	return nil
}

func (fw *FileWatcher) handleRestore(ctx context.Context, container *containerd.Containerd, trigger config.FileTrigger, triggerPath string) error {
	// Query container state to get PID
	queryResp, err := fw.client.Query(ctx, &daemon.QueryReq{
		Type: "containerd",
		Containerd: &containerd.QueryReq{
			IDs: []string{container.ID},
		},
	})
	if err != nil {
		return fmt.Errorf("failed to query container: %w", err)
	}

	if len(queryResp.States) == 0 {
		return fmt.Errorf("container has no state")
	}

	placeholderPID := int(queryResp.States[0].PID)

	// Read trigger file to get checkpoint path
	checkpointPath, err := os.ReadFile(triggerPath)
	if err != nil {
		return fmt.Errorf("failed to read checkpoint path from trigger file: %w", err)
	}

	// Build restore request
	restoreReq := &daemon.RestoreReq{
		Type: "containerd",
		Path: string(checkpointPath),
		Details: &daemon.Details{
			Containerd: container,
		},
	}

	fw.log.Info().
		Str("container_id", container.ID).
		Str("checkpoint_path", string(checkpointPath)).
		Msg("restoring container")

	restoreResp, _, err := fw.client.Restore(ctx, restoreReq)
	if err != nil {
		fw.sendSignal(placeholderPID, trigger.OnFailure, "restore failed")
		return fmt.Errorf("restore failed: %w", err)
	}

	fw.log.Info().
		Str("container_id", container.ID).
		Int("restored_pid", int(restoreResp.PID)).
		Msg("restore completed successfully")

	// Remove trigger file
	if err := os.Remove(triggerPath); err != nil {
		fw.log.Warn().Err(err).Str("path", triggerPath).Msg("failed to remove trigger file")
	}

	// Send restore complete signal to the restored process
	restoredPID := int(restoreResp.PID)
	fw.sendSignalViaPIDNamespace(placeholderPID, restoredPID, trigger.OnRestore, "restore complete")

	return nil
}

func (fw *FileWatcher) sendSignal(pid int, signalName, reason string) {
	if signalName == "" {
		return
	}

	sig := parseSignal(signalName)
	if sig == nil {
		fw.log.Warn().Str("signal", signalName).Msg("invalid signal name")
		return
	}

	process, err := os.FindProcess(pid)
	if err != nil {
		fw.log.Error().Err(err).Int("pid", pid).Msg("failed to find process")
		return
	}

	if err := process.Signal(*sig); err != nil {
		fw.log.Error().Err(err).Int("pid", pid).Str("signal", signalName).Msg("failed to send signal")
		return
	}

	fw.log.Info().
		Int("pid", pid).
		Str("signal", signalName).
		Str("reason", reason).
		Msg("sent signal")
}

func (fw *FileWatcher) sendSignalViaPIDNamespace(placeholderPID, targetPID int, signalName, reason string) {
	if signalName == "" {
		return
	}

	sig := parseSignal(signalName)
	if sig == nil {
		fw.log.Warn().Str("signal", signalName).Msg("invalid signal name")
		return
	}

	fw.log.Info().
		Int("placeholder_pid", placeholderPID).
		Int("target_pid", targetPID).
		Str("signal", signalName).
		Str("reason", reason).
		Msg("sending signal via PID namespace")

	// Use nsenter to enter the PID namespace of the placeholder container
	// and send the signal to the restored process (same pattern as Dynamo)
	cmd := exec.Command("nsenter",
		"-t", strconv.Itoa(placeholderPID),  // Target PID for namespace
		"-p",                                 // Enter PID namespace
		"--",                                 // End of nsenter options
		"kill",                               // Command to run in namespace
		fmt.Sprintf("-%d", int(*sig)),        // Signal number (more reliable than name)
		strconv.Itoa(targetPID),              // Target PID (in container namespace)
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		fw.log.Error().
			Err(err).
			Int("placeholder_pid", placeholderPID).
			Int("target_pid", targetPID).
			Str("signal", signalName).
			Str("output", string(output)).
			Msg("failed to send signal via nsenter")
		return
	}

	fw.log.Info().
		Int("placeholder_pid", placeholderPID).
		Int("target_pid", targetPID).
		Str("signal", signalName).
		Str("reason", reason).
		Msg("successfully sent signal via PID namespace")
}

func parseSignal(name string) *syscall.Signal {
	switch name {
	case "SIGUSR1", "USR1":
		sig := syscall.SIGUSR1
		return &sig
	case "SIGUSR2", "USR2":
		sig := syscall.SIGUSR2
		return &sig
	case "SIGCONT", "CONT":
		sig := syscall.SIGCONT
		return &sig
	case "SIGTERM", "TERM":
		sig := syscall.SIGTERM
		return &sig
	case "SIGKILL", "KILL":
		sig := syscall.SIGKILL
		return &sig
	case "SIGHUP", "HUP":
		sig := syscall.SIGHUP
		return &sig
	default:
		// Try parsing as number
		if num, err := strconv.Atoi(name); err == nil && num > 0 && num < 32 {
			sig := syscall.Signal(num)
			return &sig
		}
		return nil
	}
}
