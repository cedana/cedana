package gpu

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	criu_proto "buf.build/gen/go/cedana/criu/protocolbuffers/go/criu"
	"github.com/cedana/cedana/pkg/criu"
	"github.com/cedana/cedana/pkg/plugins"
	"github.com/cedana/cedana/pkg/types"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

// Chose a cmd file arch for communicating to the single proc thread to avoid dumping tcp sockets.
// The split architecture starts the GPU dump in PreDumpFunc
// and lets it run alongside criu's dump, which is safe because the controller is a separate
// process. Here the checkpoint runs on a thread of the process criu is about to seize, so it
// has to finish before criu starts: both the freeze and the dump happen in InitializeDumpFunc.

// We need to iterate over CRIU on making the gpu dump concurrent with the CRIU dump.
var _ Manager = (*ManagerSingleProc)(nil)

type ManagerSingleProc struct {
	mu       sync.RWMutex
	attached map[uint32]string // PID -> ID
	pending  map[string]bool   // IDs handed out but not yet bound to a PID

	plugins plugins.Manager
	wg      *sync.WaitGroup
}

const (
	singleProcControlPrefix = "cedana-gpu-sp-"
	// written by each replay into the image directory: "ok" or "err <error_desc>".
	replayStatusPrefix = "cedana-replay-status-"
	// published by each task that needs a replay
	replayEntryPrefix = "cedana-replay-entry-"
	// gpu state written with criu image dump
	gpuImagePrefix         = "gpu-"
	controlPollInterval    = 20 * time.Millisecond
	controlFreezeTimeout   = 2 * time.Minute
	controlDumpTimeout     = 30 * time.Minute
	controlUnfreezeTimeout = 30 * time.Second
	controlProfileTimeout  = 30 * time.Second
)

func NewSingleProcManager(ctx context.Context, serverWg *sync.WaitGroup, plugins plugins.Manager) (*ManagerSingleProc, error) {
	return &ManagerSingleProc{
		attached: make(map[uint32]string),
		pending:  make(map[string]bool),
		plugins:  plugins,
		wg:       serverWg,
	}, nil
}

// ControlPrefix is the path the application's control thread polls. The interposed library is
// told about it through CEDANA_GPU_CONTROL, so both sides derive it from the ID alone.
func ControlPrefix(id string) string {
	return filepath.Join(os.TempDir(), singleProcControlPrefix+id)
}

// DumpProfile asks every rank of a job to write out its CUDA TSC profile, and returns once they
// have. The files land in each rank's GPU log directory as cedana-tsc-profile-<pid>.log.
func DumpProfile(ctx context.Context, id string) error {
	return send(ctx, id, "profile", controlProfileTimeout)
}

// send writes a command and waits for the reply the application leaves behind. The reply file
// is written to a temporary and renamed, so a partial read is not possible.
func send(ctx context.Context, id string, cmd string, timeout time.Duration) error {
	prefix := ControlPrefix(id)
	cmdPath, ackPath := prefix+".cmd", prefix+".ack"

	os.Remove(ackPath)

	token := time.Now().UnixNano()
	line := fmt.Sprintf("%d %s\n", token, cmd)

	if err := os.WriteFile(cmdPath, []byte(line), 0o644); err != nil {
		return fmt.Errorf("failed to send %q to GPU control: %w", cmd, err)
	}
	if err := os.Chmod(cmdPath, 0o644); err != nil {
		log.Warn().Err(err).Msg("could not make the GPU control command readable")
	}

	deadline := time.Now().Add(timeout)
	for {
		if reply, err := os.ReadFile(ackPath); err == nil {
			os.Remove(ackPath)
			text := strings.TrimSpace(string(reply))
			if text == "ok" {
				return nil
			}
			return fmt.Errorf("GPU control rejected %q: %s", cmd, text)
		}
		if time.Now().After(deadline) {
			os.Remove(cmdPath)
			return fmt.Errorf("timed out after %s waiting for GPU control to answer %q", timeout, cmd)
		}
		select {
		case <-ctx.Done():
			os.Remove(cmdPath)
			return ctx.Err()
		case <-time.After(controlPollInterval):
		}
	}
}

func (m *ManagerSingleProc) Attach(ctx context.Context, pid <-chan uint32) (string, error) {
	if gpuPlugin := m.plugins.Get("gpu"); !gpuPlugin.IsInstalled() {
		return "", fmt.Errorf("Please install the GPU plugin to use GPU support")
	}

	id := uuid.NewString()

	m.mu.Lock()
	m.pending[id] = true
	m.mu.Unlock()

	// Nothing is spawned in single proc arch, we just need to know about the PID
	m.wg.Go(func() {
		var boundPID uint32
		var ok bool
		select {
		case <-ctx.Done():
		case boundPID, ok = <-pid:
		}

		m.mu.Lock()
		delete(m.pending, id)
		if ok {
			m.attached[boundPID] = id
		}
		m.mu.Unlock()

		if ok {
			log.Debug().Str("ID", id).Uint32("PID", boundPID).Msg("attached single-process GPU interception")
		} else {
			log.Debug().Str("ID", id).Msg("single-process GPU attach cancelled")
			cleanupControlFiles(id)
		}
	})

	return id, nil
}

// rebind id on restore
func (m *ManagerSingleProc) AdoptOnPID(ctx context.Context, pid <-chan uint32, id string) {
	m.wg.Go(func() {
		var boundPID uint32
		var ok bool
		select {
		case <-ctx.Done():
		case boundPID, ok = <-pid:
		}
		if !ok {
			return
		}
		m.mu.Lock()
		m.attached[boundPID] = id
		m.mu.Unlock()
		log.Debug().Str("ID", id).Uint32("PID", boundPID).Msg("adopted restored single-process GPU job")
	})
}

// nccl type
func coordSegmentPath(id string) string {
	return filepath.Join("/dev/shm", "cedana-gpu."+id+".nccl")
}

const coordSegmentImage = "gpu-coord-segment"

func saveCoordSegment(id, dir string) error {
	data, err := os.ReadFile(coordSegmentPath(id))
	if err != nil {
		if os.IsNotExist(err) {
			return nil // a job that never coordinated has none
		}
		return fmt.Errorf("failed to read GPU coordination segment: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, coordSegmentImage), data, 0o666); err != nil {
		return fmt.Errorf("failed to save GPU coordination segment: %w", err)
	}
	return nil
}

// restoreCoordSegment puts the segment back before criu maps it.
func restoreCoordSegment(id, dir string) error {
	data, err := os.ReadFile(filepath.Join(dir, coordSegmentImage))
	if err != nil {
		if os.IsNotExist(err) {
			return nil // checkpoint predates this, or the job never coordinated
		}
		return fmt.Errorf("failed to read saved GPU coordination segment: %w", err)
	}
	path := coordSegmentPath(id)
	if err := os.WriteFile(path, data, 0o666); err != nil {
		return fmt.Errorf("failed to restore GPU coordination segment: %w", err)
	}
	return os.Chmod(path, 0o666)
}

func cleanupControlFiles(id string) {
	prefix := ControlPrefix(id)
	os.Remove(prefix + ".cmd")
	os.Remove(prefix + ".ack")
	os.Remove(filepath.Join("/dev/shm", "cedana-gpu."+id+".nccl"))
}

func (m *ManagerSingleProc) Detach(ctx context.Context, pid uint32) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	id, ok := m.attached[pid]
	if !ok {
		return fmt.Errorf("no GPU interception attached to PID %d", pid)
	}
	delete(m.attached, pid)
	cleanupControlFiles(id)
	return nil
}

func (m *ManagerSingleProc) IsAttached(pid uint32) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.attached[pid]
	return ok
}

func (m *ManagerSingleProc) GetID(pid uint32) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.attached[pid]
}

// only external state is if this PID exists
func (m *ManagerSingleProc) Sync(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for pid, id := range m.attached {
		if _, err := os.Stat(fmt.Sprintf("/proc/%d", pid)); err != nil {
			log.Debug().Str("ID", id).Uint32("PID", pid).Msg("single-process GPU job is gone")
			delete(m.attached, pid)
			cleanupControlFiles(id)
		}
	}
	return nil
}

func (m *ManagerSingleProc) Checks() types.Checks {
	check := func(ctx context.Context) []*daemon.HealthCheckComponent {
		component := &daemon.HealthCheckComponent{Name: "status"}
		gpuPlugin := m.plugins.Get("gpu")
		if !gpuPlugin.IsInstalled() {
			component.Data = "missing"
			component.Errors = append(component.Errors, "Please install the GPU plugin to use GPU support.")
			return []*daemon.HealthCheckComponent{component}
		}
		if paths := gpuPlugin.LibraryPaths(); len(paths) == 0 {
			component.Data = "invalid"
			component.Errors = append(component.Errors, "GPU plugin has no library. Try reinstalling plugin.")
			return []*daemon.HealthCheckComponent{component}
		} else if _, err := os.Stat(paths[0]); err != nil {
			component.Data = "invalid"
			component.Errors = append(component.Errors, fmt.Sprintf("Invalid library: %v. Try reinstalling plugin.", err))
			return []*daemon.HealthCheckComponent{component}
		}
		component.Data = "single-process"
		return []*daemon.HealthCheckComponent{component}
	}

	tcpReuse := func(ctx context.Context) []*daemon.HealthCheckComponent {
		component := &daemon.HealthCheckComponent{Name: "tcp_tw_reuse"}
		val, err := os.ReadFile("/proc/sys/net/ipv4/tcp_tw_reuse")
		if err != nil {
			component.Data = "unknown"
			component.Warnings = append(component.Warnings,
				fmt.Sprintf("could not read net.ipv4.tcp_tw_reuse: %v", err))
			return []*daemon.HealthCheckComponent{component}
		}
		setting := strings.TrimSpace(string(val))
		component.Data = setting

		// TODO BS: this needs to be enabled at cedana install of the GPU plugin on a multi rank node
		if setting != "1" {
			component.Warnings = append(component.Warnings,
				fmt.Sprintf("net.ipv4.tcp_tw_reuse is %s: restoring a multi-rank GPU job soon "+
					"after checkpointing it will fail to re-bind its ranks' connections. "+
					"Set it to 1 (sysctl -w net.ipv4.tcp_tw_reuse=1).", setting))
		}
		return []*daemon.HealthCheckComponent{component}
	}

	return types.Checks{Name: "gpu", List: []types.Check{check, tcpReuse}}
}

func (m *ManagerSingleProc) Freeze(ctx context.Context, pid uint32) error {
	id := m.GetID(pid)
	if id == "" {
		return fmt.Errorf("no GPU interception attached to PID %d", pid)
	}
	return send(ctx, id, "freeze", controlFreezeTimeout)
}

func (m *ManagerSingleProc) Unfreeze(ctx context.Context, pid uint32) error {
	id := m.GetID(pid)
	if id == "" {
		return fmt.Errorf("no GPU interception attached to PID %d", pid)
	}
	return send(ctx, id, "unfreeze", controlFreezeTimeout)
}

func failedReplays(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return []string{fmt.Sprintf("cannot read the image directory %s: %v", dir, err)}
	}

	expected := []string{}
	statuses := map[string]string{}
	gpuImages := 0
	for _, e := range entries {
		name := e.Name()
		switch {
		case strings.HasPrefix(name, gpuImagePrefix):
			gpuImages++
		case strings.HasPrefix(name, replayEntryPrefix):
			expected = append(expected, strings.TrimPrefix(name, replayEntryPrefix))
		case strings.HasPrefix(name, replayStatusPrefix):
			pid := strings.TrimPrefix(name, replayStatusPrefix)
			body, err := os.ReadFile(filepath.Join(dir, name))
			if err != nil {
				statuses[pid] = fmt.Sprintf("unreadable status: %v", err)
				continue
			}
			statuses[pid] = strings.TrimSpace(string(body))
		}
	}

	if len(expected) == 0 {
		if gpuImages > 0 {
			return []string{fmt.Sprintf(
				"%d GPU images in %s but no replay entries, so nothing would have restored "+
					"them", gpuImages, dir)}
		}
		return nil // no GPU state in this checkpoint
	}

	var failures []string
	for _, pid := range expected {
		status, ok := statuses[pid]
		if !ok {
			failures = append(failures,
				fmt.Sprintf("task %s: its replay left no status, so it never ran or never "+
					"finished", pid))
			continue
		}
		if status != "ok" {
			failures = append(failures, fmt.Sprintf("task %s: %s", pid, status))
		}
	}
	return failures
}

func (m *ManagerSingleProc) CRIUCallback(id string) *criu.NotifyCallback {
	callback := &criu.NotifyCallback{Name: "gpu-singleproc"}
	log := log.With().Str("plugin", "gpu").Str("ID", id).Logger()

	callback.InitializeDumpFunc = func(ctx context.Context, opts *criu_proto.CriuOpts) error {
		log.Info().Msg("GPU freeze starting")
		if err := send(ctx, id, "freeze", controlFreezeTimeout); err != nil {
			return err
		}
		log.Info().Msg("GPU freeze complete, dumping GPU state")

		if err := send(ctx, id, "dump "+opts.GetImagesDir(), controlDumpTimeout); err != nil {
			// Leave nothing frozen behind if the dump could not be taken.
			if uerr := send(context.WithoutCancel(ctx), id, "unfreeze", controlFreezeTimeout); uerr != nil {
				log.Warn().Err(uerr).Msg("failed to unfreeze after a failed GPU dump")
			}
			return err
		}
		if err := saveCoordSegment(id, opts.GetImagesDir()); err != nil {
			log.Warn().Err(err).Msg("failed to save the GPU coordination segment")
		}
		log.Info().Msg("GPU dump complete")
		return nil
	}

	callback.FinalizeDumpFunc = func(ctx context.Context, opts *criu_proto.CriuOpts, dumpErr error) error {
		if !opts.GetLeaveRunning() {
			return dumpErr
		}
		if err := send(context.WithoutCancel(ctx), id, "unfreeze", controlUnfreezeTimeout); err != nil {
			log.Warn().Err(err).Msg("failed to unfreeze after dump")
		}
		return dumpErr
	}

	// criu maps the coordination segment by name, so it has to exist before the restore starts.
	callback.InitializeRestoreFunc = func(ctx context.Context, opts *criu_proto.CriuOpts) error {
		if err := restoreCoordSegment(id, opts.GetImagesDir()); err != nil {
			return err
		}
		return nil
	}

	callback.FinalizeRestoreFunc = func(ctx context.Context, opts *criu_proto.CriuOpts, restoreErr error) error {
		if restoreErr != nil {
			return restoreErr
		}
		if failures := failedReplays(opts.GetImagesDir()); len(failures) > 0 {
			for _, f := range failures {
				log.Error().Msg("GPU replay failed: " + f)
			}
			return fmt.Errorf("the GPU state was not restored: %s", strings.Join(failures, "; "))
		}
		return nil
	}

	// gate close dafuq
	callback.PostResumeFunc = func(ctx context.Context) error {
		log.Info().Msg("releasing restored GPU job")
		return send(ctx, id, "unfreeze", controlUnfreezeTimeout)
	}

	return callback
}
