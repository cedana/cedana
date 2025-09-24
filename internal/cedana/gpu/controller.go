package gpu

// Internal definitions for the GPU controller

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"sync"
	"syscall"
	"time"

	"buf.build/gen/go/cedana/cedana-gpu/grpc/go/gpu/gpugrpc"
	"buf.build/gen/go/cedana/cedana-gpu/protocolbuffers/go/gpu"
	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	criu_proto "buf.build/gen/go/cedana/criu/protocolbuffers/go/criu"
	"github.com/cedana/cedana/pkg/config"
	criu_client "github.com/cedana/cedana/pkg/criu"
	"github.com/cedana/cedana/pkg/types"
	"github.com/cedana/cedana/pkg/utils"
	"github.com/gofrs/flock"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"
)

const (
	CONTROLLER_PROCESS_NAME                = "cedana-gpu-controller"
	CONTROLLER_ADDRESS_FORMATTER           = "unix://%s/cedana-gpu-controller-%s.sock"
	CONTROLLER_SOCKET_FORMATTER            = "%s/cedana-gpu-controller-%s.sock"
	CONTROLLER_SOCKET_PATTERN              = "cedana-gpu-controller-(.*).sock"
	CONTROLLER_SHM_FILE_FORMATTER          = "/dev/shm/cedana-gpu.%s"
	CONTROLLER_SHM_FILE_PATTERN            = "/dev/shm/cedana-gpu.(.*)"
	CONTROLLER_HOSTMEM_FILE_FORMATTER      = "/dev/shm/cedana-gpu.%s.misc/hostmem-%d"
	CONTROLLER_HOSTMEM_FILE_PATTERN        = "/dev/shm/cedana-gpu.(.*).misc/hostmem-(\\d+)"
	CONTROLLER_BOOKING_LOCK_FILE_FORMATTER = "/dev/shm/cedana-gpu.%s.booking"
	CONTROLLER_DEFAULT_FREEZE_TYPE         = gpu.FreezeType_FREEZE_TYPE_IPC
	CONTROLLER_TERMINATE_SIGNAL            = syscall.SIGTERM
	CONTROLLER_RESTORE_NEW_PID_SIGNAL      = syscall.SIGUSR1      // Signal to the restored process to notify it has a new PID
	CONTROLLER_CHECK_SHM_SIZE              = 100 * utils.MEBIBYTE // Small size to run controller health check

	FREEZE_TIMEOUT   = 1 * time.Minute
	UNFREEZE_TIMEOUT = 1 * time.Minute
	DUMP_TIMEOUT     = 5 * time.Minute
	RESTORE_TIMEOUT  = 5 * time.Minute
	HEALTH_TIMEOUT   = 30 * time.Second
	INFO_TIMEOUT     = 30 * time.Second

	// Whether to do GPU dump and restore in parallel to CRIU dump and restore.
	PARALLEL_DUMP    = true
	PARALLEL_RESTORE = true
)

type controller struct {
	ID          string
	PID         uint32
	ParentPID   uint32
	Address     string
	AttachedPID uint32
	ShmSize     uint64
	ShmName     string
	UID         uint32
	GID         uint32
	Version     string

	ErrBuf      *bytes.Buffer
	Booking     *flock.Flock // To book the controller for use
	Termination sync.Mutex   // To protect termination
	gpugrpc.ControllerClient
	*grpc.ClientConn
}

///////////////////////
/// CONTROLLER POOL ///
///////////////////////

type pool struct {
	sync.Map
}

// Get a specific GPU controller
func (p *pool) Get(id string) *controller {
	c, ok := p.Load(id)
	if !ok {
		return nil
	}
	return c.(*controller)
}

// Finds the GPU controller for the attached PID
func (p *pool) Find(attachedPID uint32) *controller {
	var found *controller
	p.Range(func(key, value any) bool {
		c := value.(*controller)
		if c.AttachedPID == attachedPID {
			found = c
			return false
		}
		return true
	})
	return found
}

// Returns a list of all GPU controllers grouped by free, pending, and busy states
func (p *pool) List() (free []*controller, busy []*controller, remaining []*controller) {
	p.Range(func(key, value any) bool {
		c := value.(*controller)
		if utils.PidRunning(c.PID) {
			if c.Booking.Locked() {
				busy = append(busy, c)
				return true
			}
			if c.AttachedPID == 0 {
				shmSizeMatches := c.ShmSize == uint64(config.Global.GPU.ShmSize)
				credentialsMatch := c.UID == uint32(os.Getuid()) && c.GID == uint32(os.Getgid())
				if shmSizeMatches && credentialsMatch {
					free = append(free, c)
					return true
				}
			} else if utils.PidRunning(c.AttachedPID) {
				busy = append(busy, c)
				return true
			}
		}
		remaining = append(remaining, c)
		return true
	})
	return free, busy, remaining
}

// Sync with all existing GPU controllers in the system
func (p *pool) Sync(ctx context.Context) (err error) {
	list, err := os.ReadDir(config.Global.GPU.SockDir)
	if err != nil {
		return fmt.Errorf("failed to read GPU sock directory: %w", err)
	}

	wg := sync.WaitGroup{}

	for _, entry := range list {
		name := entry.Name()
		matches := regexp.MustCompile(CONTROLLER_SOCKET_PATTERN).FindStringSubmatch(name)
		if len(matches) < 2 {
			continue
		}
		id := matches[1]
		if id == "" {
			continue
		}

		c := p.Get(id)

		if c == nil {
			fileInfo, err := os.Stat(fmt.Sprintf(CONTROLLER_SOCKET_FORMATTER, config.Global.GPU.SockDir, id))
			if err != nil {
				continue
			}
			c = &controller{
				ID:      id,
				Address: fmt.Sprintf(CONTROLLER_ADDRESS_FORMATTER, config.Global.GPU.SockDir, id),
				Booking: flock.New(fmt.Sprintf(CONTROLLER_BOOKING_LOCK_FILE_FORMATTER, id), flock.SetFlag(os.O_CREATE|os.O_RDWR)),
				UID:     fileInfo.Sys().(*syscall.Stat_t).Uid,
				GID:     fileInfo.Sys().(*syscall.Stat_t).Gid,
			}
		} else if c.Booking.Locked() {
			continue
		}

		wg.Add(1)
		go func() {
			err := c.Connect(ctx, false)
			if err == nil {
				p.Store(id, c)
			}
			wg.Done()
		}()
	}

	wg.Wait()

	return nil
}

// Books a free GPU controller, and marks it as booked.
func (p *pool) Book() *controller {
	free, _, _ := p.List()
	if len(free) == 0 {
		return nil
	}
	for _, c := range free {
		if acquired, _ := c.Booking.TryLock(); acquired {
			return c
		}
	}
	return nil
}

// Spawns a GPU controller, and marks it as booked.
func (p *pool) Spawn(ctx context.Context, binary string, env ...string) (c *controller, err error) {
	id := uuid.NewString()

	observability := ""
	if config.Global.GPU.Observability {
		observability = "--observability"
	}

	c = &controller{
		ID:     id,
		ErrBuf: &bytes.Buffer{},
		UID:    uint32(os.Getuid()),
		GID:    uint32(os.Getgid()),
	}

	cmd := exec.Command(
		binary,
		id,
		observability,
		"--log-dir",
		config.Global.GPU.LogDir,
		"--sock-dir",
		config.Global.GPU.SockDir,
	)

	// We create a new process group and session to essentially daemonize the controller process.
	// So that workers of the controller can all be signaled together.

	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid:                     true,  // Create a new session and process group for the controller
		GidMappingsEnableSetgroups: false, // Avoid permission issues when running as non-root user
	}

	cmd.Stderr = c.ErrBuf

	existingLD := os.Getenv("LD_LIBRARY_PATH")
	ldPath := config.Global.GPU.LdLibPath
	if existingLD != "" {
		ldPath = existingLD + ":" + ldPath
	}

	cmd.Env = append(
		os.Environ(),
		"CEDANA_URL="+config.Global.Connection.URL,
		"CEDANA_AUTH_TOKEN="+config.Global.Connection.AuthToken,
		"CEDANA_GPU_SHM_SIZE="+fmt.Sprintf("%v", config.Global.GPU.ShmSize),
		"LD_LIBRARY_PATH="+ldPath,
	)

	cmd.Env = append(cmd.Env, env...)

	c.Address = fmt.Sprintf(CONTROLLER_ADDRESS_FORMATTER, config.Global.GPU.SockDir, id)
	c.Booking = flock.New(fmt.Sprintf(CONTROLLER_BOOKING_LOCK_FILE_FORMATTER, id), flock.SetFlag(os.O_CREATE|os.O_RDWR))
	err = c.Booking.Lock() // Locked until whoever spawned us sets us free
	if err != nil {
		return nil, fmt.Errorf("failed to lock GPU controller: %w", err)
	}

	p.Store(id, c)

	defer func() {
		if err != nil {
			p.Terminate(ctx, id)
		}
	}()

	err = cmd.Start()
	if err != nil {
		return nil, fmt.Errorf(
			"failed to start GPU controller: %w",
			utils.GRPCErrorShort(err, c.ErrBuf.String()),
		)
	}

	c.PID = uint32(cmd.Process.Pid)
	c.ParentPID = uint32(os.Getpid())

	err = c.Connect(ctx, true)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to GPU controller: %w", err)
	}

	return c, err
}

func (p *pool) Terminate(ctx context.Context, id string) {
	c := p.Get(id)
	if c == nil {
		return
	}

	log.Debug().Str("ID", id).Uint32("PID", c.PID).Uint32("AttachedPID", c.AttachedPID).Msg("terminating GPU controller")

	defer os.Remove(fmt.Sprintf(CONTROLLER_SOCKET_FORMATTER, config.Global.GPU.SockDir, id))
	defer os.Remove(fmt.Sprintf(CONTROLLER_BOOKING_LOCK_FILE_FORMATTER, id))

	c.Termination.Lock()
	defer c.Termination.Unlock()

	p.Delete(id) // Remove from the pool

	if c.PID == 0 {
		return
	}
	syscall.Kill(-int(c.PID), CONTROLLER_TERMINATE_SIGNAL) // To process group so even its workers are killed
	if c.ClientConn != nil {
		c.ClientConn.Close()
		c.ClientConn = nil
		c.ControllerClient = nil
	}
	if int(c.ParentPID) == os.Getpid() { // If we spawned it, then reap it
		process, err := os.FindProcess(int(c.PID))
		if err != nil {
			return
		}
		state, err := process.Wait()
		if err != nil {
			log.Trace().Err(err).Str("ID", id).Uint32("PID", c.PID).Msg("GPU controller Wait()")
		}
		log.Debug().Str("ID", id).Uint32("PID", c.PID).Int("status", state.ExitCode()).Msg("GPU controller exited")
	} else { // Otherwise, just wait for it to exit
		waitCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		<-utils.WaitForPidCtx(waitCtx, c.PID)
		log.Debug().Str("ID", id).Uint32("PID", c.PID).Str("status", "unknown").Msg("GPU controller exited")
	}
}

func (p *pool) CRIUCallback(id string, freezeType ...gpu.FreezeType) *criu_client.NotifyCallback {
	callback := &criu_client.NotifyCallback{Name: "gpu"}

	// Add pre-dump hook for GPU dump. We freeze the GPU controller so we can
	// do the GPU dump in parallel to CRIU dump.
	dumpErr := make(chan error, 1)
	callback.PreDumpFunc = func(ctx context.Context, opts *criu_proto.CriuOpts) error {
		waitCtx, cancel := context.WithTimeout(ctx, FREEZE_TIMEOUT)
		defer cancel()

		pid := uint32(opts.GetPid())

		controller := p.Get(id)
		if controller == nil {
			return fmt.Errorf("GPU controller not found, is the process still running?")
		}

		// Required to ensure the controller does not get terminated while dumping. Otherwise, CRIU might discover
		// 'ghost files' as the GPU controller deletes the shared memory file on termination.
		controller.Termination.Lock()

		freezeType = append(freezeType, CONTROLLER_DEFAULT_FREEZE_TYPE) // Default to IPC freeze type if not provided

		log.Debug().Str("ID", id).Uint32("PID", pid).Str("type", freezeType[0].String()).Msg("GPU freeze starting")

		_, err := controller.Freeze(waitCtx, &gpu.FreezeReq{Type: freezeType[0]})
		if err != nil {
			log.Error().Err(err).Str("ID", id).Uint32("PID", pid).Msg("failed to freeze GPU")
			return fmt.Errorf("failed to freeze GPU: %v", utils.GRPCError(err))
		}

		log.Info().Str("ID", id).Uint32("PID", pid).Str("type", freezeType[0].String()).Msg("GPU freeze complete")

		// Begin GPU dump in parallel to CRIU dump

		go func() {
			defer close(dumpErr)

			waitCtx, cancel = context.WithTimeout(ctx, DUMP_TIMEOUT)
			defer cancel()

			log.Debug().Str("ID", id).Uint32("PID", pid).Msg("GPU dump starting")

			_, err := controller.Dump(waitCtx, &gpu.DumpReq{Dir: opts.GetImagesDir(), Stream: opts.GetStream(), LeaveRunning: opts.GetLeaveRunning()})
			if err != nil {
				log.Error().Err(err).Str("ID", id).Uint32("PID", pid).Msg("failed to dump GPU")
				dumpErr <- fmt.Errorf("failed to dump GPU: %v", utils.GRPCError(err))
				return
			}
			log.Info().Str("ID", id).Uint32("PID", pid).Msg("GPU dump complete")
		}()
		if PARALLEL_DUMP {
			return nil
		}
		return utils.GRPCError(<-dumpErr)
	}

	// Wait for GPU dump to finish before finalizing the dump
	callback.PostDumpFunc = func(ctx context.Context, opts *criu_proto.CriuOpts) error {
		waitCtx, cancel := context.WithTimeout(ctx, UNFREEZE_TIMEOUT)
		defer cancel()

		pid := uint32(opts.GetPid())

		controller := p.Get(id)
		if controller == nil {
			return fmt.Errorf("GPU controller not found, is the process still running?")
		}

		defer controller.Termination.Unlock()

		log.Debug().Str("ID", controller.ID).Uint32("PID", pid).Msg("GPU unfreeze starting")

		_, err := controller.Unfreeze(waitCtx, &gpu.UnfreezeReq{})
		if err != nil {
			log.Error().Err(err).Str("ID", controller.ID).Uint32("PID", pid).Msg("failed to unfreeze GPU")
		} else {
			log.Info().Str("ID", controller.ID).Uint32("PID", pid).Msg("GPU unfreeze completed")
		}

		return errors.Join(err, utils.GRPCError(<-dumpErr))
	}

	// Unfreeze on dump failure as well
	callback.OnDumpErrorFunc = func(ctx context.Context, opts *criu_proto.CriuOpts) {
		waitCtx, cancel := context.WithTimeout(ctx, UNFREEZE_TIMEOUT)
		defer cancel()

		pid := uint32(opts.GetPid())

		controller := p.Get(id)
		if controller == nil {
			log.Error().Uint32("PID", pid).Msg("GPU controller not found, is the process still running?")
			return
		}

		controller.Termination.TryLock() // Might be already locked, so ensure we don't deadlock
		defer controller.Termination.Unlock()

		log.Debug().Str("ID", controller.ID).Uint32("PID", pid).Msg("GPU unfreeze starting")

		_, err := controller.Unfreeze(waitCtx, &gpu.UnfreezeReq{})
		if err != nil {
			log.Error().Err(err).Str("ID", controller.ID).Uint32("PID", pid).Msg("failed to unfreeze GPU")
		} else {
			log.Info().Str("ID", controller.ID).Uint32("PID", pid).Msg("GPU unfreeze completed")
		}
	}

	// Add pre-restore hook for GPU restore, that begins GPU restore in parallel
	// to CRIU restore. We instead block at post-restore, to maximize concurrency.
	restoreErr := make(chan error, 1)
	callback.PreRestoreFunc = func(ctx context.Context, opts *criu_proto.CriuOpts) error {
		go func() {
			defer close(restoreErr)

			waitCtx, cancel := context.WithTimeout(ctx, RESTORE_TIMEOUT)
			defer cancel()

			controller := p.Get(id)
			if controller == nil {
				restoreErr <- fmt.Errorf("GPU controller not found, is the process still running?")
				return
			}

			controller.Termination.Lock() // Required to ensure the controller does not get terminated while restoring
			defer controller.Termination.Unlock()

			log.Debug().Str("ID", controller.ID).Msg("GPU restore starting")

			_, err := controller.Restore(waitCtx, &gpu.RestoreReq{Dir: opts.GetImagesDir(), Stream: opts.GetStream()})
			if err != nil {
				log.Error().Err(err).Str("ID", controller.ID).Msg("failed to restore GPU")
				restoreErr <- fmt.Errorf("failed to restore GPU: %v", utils.GRPCError(err))
				return
			}
			log.Info().Str("ID", controller.ID).Msg("GPU restore complete")

			// FIXME: It's not correct to add the below as components to the parent (PreRestoreFunc). Because
			// the restore happens inside a goroutine, the timing components belong to the restore goroutine (concurrent).

			// copyMemTime := time.Duration(resp.GetRestoreStats().GetCopyMemTime()) * time.Millisecond
			// replayCallsTime := time.Duration(resp.GetRestoreStats().GetReplayCallsTime()) * time.Millisecond
			// profiling.AddTimingComponent(ctx, copyMemTime, "controller.CopyMemory")
			// profiling.AddTimingComponent(ctx, replayCallsTime, "controller.ReplayCalls")
		}()
		if PARALLEL_RESTORE {
			return nil
		}
		return utils.GRPCError(<-restoreErr)
	}

	restoredPid := make(chan int32, 1)

	// Wait for GPU restore to finish before resuming the process
	callback.PostRestoreFunc = func(ctx context.Context, pid int32) error {
		restoredPid <- pid
		close(restoredPid)

		return utils.GRPCError(<-restoreErr)
	}

	// Signal the process so it knowns it may have a new PID (only useful for containers which get
	// restore with a different host PID).
	callback.PreResumeFunc = func(ctx context.Context) error {
		return syscall.Kill(int(<-restoredPid), CONTROLLER_RESTORE_NEW_PID_SIGNAL)
	}

	return callback
}

func (p *pool) Check(binary string) types.Check {
	return func(ctx context.Context) []*daemon.HealthCheckComponent {
		component := &daemon.HealthCheckComponent{Name: "status"}

		controller, err := p.Spawn(ctx, binary, fmt.Sprintf("CEDANA_GPU_SHM_SIZE=%d", CONTROLLER_CHECK_SHM_SIZE))
		if err != nil {
			component.Data = "failed"
			component.Errors = append(component.Errors, err.Error())
			return []*daemon.HealthCheckComponent{component}
		}
		defer p.Terminate(ctx, controller.ID)

		components, err := controller.WaitForHealthCheck(ctx)
		if components == nil && err != nil {
			component.Data = "failed"
			component.Errors = append(component.Errors, fmt.Sprintf("Failed health check: %v", err))
			return []*daemon.HealthCheckComponent{component}
		}

		component.Data = "available"

		return append([]*daemon.HealthCheckComponent{component}, components...)
	}
}

//////////////////
/// CONTROLLER ///
//////////////////

// Connect connects to the GPU controller. If already connected, it will refresh the controller info.
func (c *controller) Connect(ctx context.Context, wait bool) (err error) {
	if c.Address == "" {
		return fmt.Errorf("controller address is not set")
	}

	if c.ClientConn == nil || c.ClientConn.GetState() == connectivity.Shutdown {
		opts := []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}
		conn, err := grpc.NewClient(c.Address, opts...)
		if err != nil {
			return fmt.Errorf(
				"failed to create GPU controller client: %w",
				utils.GRPCErrorShort(err, c.ErrBuf.String()),
			)
		}
		c.ClientConn = conn
		c.ControllerClient = gpugrpc.NewControllerClient(conn)

	}

	var info *gpu.InfoResp

	if wait {
		info, err = c.WaitForInfo(ctx)
	} else {
		info, err = c.Info(ctx, &gpu.InfoReq{})
	}
	if err != nil {
		return err
	}

	if c.AttachedPID == 0 {
		c.AttachedPID = info.GetAttachedPID()
	}
	c.ShmSize = info.GetShmSize()
	c.ShmName = info.GetShmName()
	c.Version = info.GetVersion()
	c.PID = info.GetPID()

	return err
}

// Forcefully attach to a PID, so that on next Info call, the controller will return this as the attached PID.
func (c *controller) Attach(ctx context.Context, pid uint32) (err error) {
	_, err = c.ControllerClient.Attach(ctx, &gpu.AttachReq{PID: pid})
	if err != nil {
		return utils.GRPCErrorShort(err, c.ErrBuf.String())
	}

	return nil
}

// WaitForInfo gets info from the GPU controller, blocking on connection until ready.
// This can be used as a proxy to wait for the controller to be ready.
func (c *controller) WaitForInfo(ctx context.Context) (*gpu.InfoResp, error) {
	waitCtx, cancel := context.WithTimeout(ctx, INFO_TIMEOUT)
	defer cancel()

	if c.PID != 0 {
		go func() {
			<-utils.WaitForPidCtx(waitCtx, c.PID)
			cancel()
		}()
	}

	resp, err := c.Info(waitCtx, &gpu.InfoReq{}, grpc.WaitForReady(true))
	if err != nil {
		return nil, utils.GRPCErrorShort(err, c.ErrBuf.String())
	}

	return resp, nil
}

// Health checks the GPU controller, blocking on connection until ready.
// This can be used as a proxy to wait for the controller to be ready.
func (c *controller) WaitForHealthCheck(ctx context.Context) ([]*daemon.HealthCheckComponent, error) {
	waitCtx, cancel := context.WithTimeout(ctx, HEALTH_TIMEOUT)
	defer cancel()

	// Wait for early controller exit, and cancel the blocking health check
	if c.PID != 0 {
		go func() {
			<-utils.WaitForPidCtx(waitCtx, c.PID)
			cancel()
		}()
	}

	resp, err := c.HealthCheck(waitCtx, &gpu.HealthCheckReq{}, grpc.WaitForReady(true))
	var components []*daemon.HealthCheckComponent
	if resp != nil {
		l := log.Debug()
		l.Str("ID", c.ID)
		for _, component := range resp.Components {
			l = l.Str(component.Name, component.Data)
			for _, w := range component.Warnings {
				log.Warn().Str("ID", c.ID).Str(component.Name, component.Data).Msg(w)
			}
			for _, e := range component.Errors {
				log.Error().Str("ID", c.ID).Str(component.Name, component.Data).Msg(e)
			}
			components = append(components, &daemon.HealthCheckComponent{
				Name:     component.Name,
				Data:     component.Data,
				Warnings: component.Warnings,
				Errors:   component.Errors,
			})
		}
		l.Msg("health checked GPU controller")
	}
	if err != nil {
		return components, utils.GRPCErrorShort(err, c.ErrBuf.String())
	}

	return components, nil
}
