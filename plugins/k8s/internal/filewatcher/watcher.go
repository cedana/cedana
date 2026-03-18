package filewatcher

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"time"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/plugins/containerd"
	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/plugins/k8s"
	"github.com/cedana/cedana/pkg/client"
	"github.com/rs/zerolog"
)

// PodWatchConfig defines file trigger configuration for a specific pod
type PodWatchConfig struct {
	Namespace   string
	PodName     string
	TriggerPath string
	OnSuccess   string
	OnRestore   string
	OnFailure   string
}

// FileWatcher polls for checkpoint trigger files in watched pods
type FileWatcher struct {
	client       *client.Client
	pollInterval time.Duration
	watches      map[string]*PodWatchConfig // key: "namespace/podName"
	watchesMu    sync.RWMutex
	log          zerolog.Logger
}

// New creates a new file watcher
func New(client *client.Client, log zerolog.Logger) *FileWatcher {
	return &FileWatcher{
		client:       client,
		pollInterval: time.Second, // Fixed 1s poll interval
		watches:      make(map[string]*PodWatchConfig),
		log:          log,
	}
}

// AddWatch adds a pod to the watch list
func (fw *FileWatcher) AddWatch(cfg *PodWatchConfig) {
	fw.watchesMu.Lock()
	defer fw.watchesMu.Unlock()

	key := fmt.Sprintf("%s/%s", cfg.Namespace, cfg.PodName)
	fw.watches[key] = cfg
	fw.log.Info().
		Str("pod", cfg.PodName).
		Str("namespace", cfg.Namespace).
		Str("trigger_path", cfg.TriggerPath).
		Msg("added pod to file watch list")
}

// RemoveWatch removes a pod from the watch list
func (fw *FileWatcher) RemoveWatch(namespace, podName string) {
	fw.watchesMu.Lock()
	defer fw.watchesMu.Unlock()

	key := fmt.Sprintf("%s/%s", namespace, podName)
	delete(fw.watches, key)
	fw.log.Info().
		Str("pod", podName).
		Str("namespace", namespace).
		Msg("removed pod from file watch list")
}

// Start begins watching for trigger files
func (fw *FileWatcher) Start(ctx context.Context) error {
	fw.log.Info().
		Dur("poll_interval", fw.pollInterval).
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
	fw.watchesMu.RLock()
	watches := make(map[string]*PodWatchConfig, len(fw.watches))
	for k, v := range fw.watches {
		watches[k] = v
	}
	fw.watchesMu.RUnlock()

	if len(watches) == 0 {
		return nil // No pods to watch
	}

	// Check each watched pod
	for _, watch := range watches {
		if err := fw.checkPod(ctx, watch); err != nil {
			// If query fails, pod might be deleted - remove from watch list
			fw.log.Warn().
				Err(err).
				Str("pod", watch.PodName).
				Str("namespace", watch.Namespace).
				Msg("failed to check pod, removing from watch list")
			fw.RemoveWatch(watch.Namespace, watch.PodName)
			continue
		}
	}

	return nil
}

func (fw *FileWatcher) checkPod(ctx context.Context, watch *PodWatchConfig) error {
	// Query pod using k8s Query (same pattern as eventstream)
	queryResp, err := fw.client.Query(ctx, &daemon.QueryReq{
		Type: "k8s",
		K8S: &k8s.QueryReq{
			Names:         []string{watch.PodName},
			Namespace:     watch.Namespace,
			ContainerType: "container",
		},
	})
	if err != nil {
		return fmt.Errorf("failed to query pod: %w", err)
	}

	if len(queryResp.K8S.Pods) == 0 {
		return fmt.Errorf("pod not found")
	}

	containers := queryResp.K8S.Pods[0].Containerd
	if len(containers) == 0 {
		return nil // No containers yet
	}

	// Check each container for trigger file
	for _, container := range containers {
		if err := fw.checkTrigger(ctx, container, watch); err != nil {
			fw.log.Error().
				Err(err).
				Str("container_id", container.GetID()).
				Str("pod", watch.PodName).
				Str("trigger_path", watch.TriggerPath).
				Msg("failed to check trigger")
		}
	}

	return nil
}

func (fw *FileWatcher) checkTrigger(ctx context.Context, container *containerd.Containerd, watch *PodWatchConfig) error {
	// Construct path to file in container's rootfs
	// For containerd, the rootfs is at <bundle>/rootfs
	if container.GetRunc() == nil {
		return fmt.Errorf("container has no runc runtime info")
	}

	rootfsPath := filepath.Join(container.GetRunc().GetBundle(), "rootfs")
	triggerPath := filepath.Join(rootfsPath, watch.TriggerPath)

	// Check if file exists
	if _, err := os.Stat(triggerPath); os.IsNotExist(err) {
		return nil // File doesn't exist, nothing to do
	} else if err != nil {
		return fmt.Errorf("failed to stat trigger file: %w", err)
	}

	// File exists! Trigger checkpoint
	fw.log.Info().
		Str("container_id", container.ID).
		Str("pod", watch.PodName).
		Str("trigger_path", watch.TriggerPath).
		Msg("trigger file detected")

	return fw.handleCheckpoint(ctx, container, watch, triggerPath)
}

func (fw *FileWatcher) handleCheckpoint(ctx context.Context, container *containerd.Containerd, watch *PodWatchConfig, triggerPath string) error {
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
		Dir:  "/tmp", // TODO: get from config or propagator
		Details: &daemon.Details{
			Containerd: container,
		},
	}

	// Perform checkpoint: Freeze → Dump → Unfreeze
	fw.log.Info().Str("container_id", container.ID).Msg("freezing container")
	if _, _, err := fw.client.Freeze(ctx, dumpReq); err != nil {
		fw.sendSignal(containerPID, watch.OnFailure, "checkpoint freeze failed")
		return fmt.Errorf("freeze failed: %w", err)
	}

	fw.log.Info().Str("container_id", container.ID).Msg("dumping container")
	dumpResp, _, err := fw.client.Dump(ctx, dumpReq)
	if err != nil {
		fw.client.Unfreeze(ctx, dumpReq) // Try to unfreeze
		fw.sendSignal(containerPID, watch.OnFailure, "checkpoint dump failed")
		return fmt.Errorf("dump failed: %w", err)
	}

	fw.log.Info().Str("container_id", container.ID).Msg("unfreezing container")
	if _, _, err := fw.client.Unfreeze(ctx, dumpReq); err != nil {
		fw.sendSignal(containerPID, watch.OnFailure, "checkpoint unfreeze failed")
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
	fw.sendSignal(containerPID, watch.OnSuccess, "checkpoint complete")

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
