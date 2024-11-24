package job

// Implements a job manager, that uses the DB as a backing store.
// Since methods cannot fail, we manage state in-memory, keeping the DB in sync
// lazily in the background with retry logic.

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"syscall"
	"time"

	"buf.build/gen/go/cedana/cedana-gpu/grpc/go/gpu/gpugrpc"
	"buf.build/gen/go/cedana/cedana-gpu/protocolbuffers/go/gpu"
	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/internal/db"
	"github.com/cedana/cedana/pkg/utils"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/proto"
)

const (
	GPU_CONTROLLER_HOST         string        = "localhost"
	GPU_CONTROLLER_WAIT_TIMEOUT time.Duration = 30 * time.Second

	// Signal sent to job when GPU controller exits prematurely. The intercepted job
	// is guaranteed to exit upon receiving this signal, and prints to stderr
	// about the GPU controller's failure.
	GPU_CONTROLLER_PREMATURE_EXIT_SIGNAL syscall.Signal = syscall.SIGUSR1
)

type ManagerDBLazy struct {
	db   db.DB
	jobs map[string]*Job

	gpuControllers map[string]*gpuController
}

type gpuController struct {
	cmd    *exec.Cmd
	client gpugrpc.ControllerClient
	conn   *grpc.ClientConn
	out    *bytes.Buffer
}

func NewManagerDBLazy(ctx context.Context, wg *sync.WaitGroup) (*ManagerDBLazy, error) {
	db, err := db.NewLocalDB(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create new local db: %w", err)
	}

	return &ManagerDBLazy{
		db:             db,
		jobs:           make(map[string]*Job),
		gpuControllers: make(map[string]*gpuController),
	}, nil
}

/////////////////
//// Methods ////
/////////////////

func (m *ManagerDBLazy) New(jid string, jobType string) (*Job, error) {
	if jid == "" {
		return nil, fmt.Errorf("missing JID")
	}

	job := newJob(jid, jobType)
	m.jobs[jid] = job

	return job, nil
}

func (m *ManagerDBLazy) Get(jid string) *Job {
	job, ok := m.jobs[jid]
	if !ok {
		return nil
	}
	return job
}

func (m *ManagerDBLazy) Delete(jid string) {
	delete(m.jobs, jid)
}

func (m *ManagerDBLazy) List(jids ...string) []*Job {
	var jobs []*Job

	jidSet := make(map[string]any)
	for _, jid := range jids {
		jidSet[jid] = nil
	}

	for _, job := range m.jobs {
		if _, ok := jidSet[job.JID]; len(jids) > 0 && !ok {
			continue
		}
		jobs = append(jobs, job)
	}
	return jobs
}

func (m *ManagerDBLazy) Exists(jid string) bool {
	_, ok := m.jobs[jid]
	return ok
}

func (m *ManagerDBLazy) Manage(
	ctx context.Context,
	wg *sync.WaitGroup,
	jid string,
	pid uint32,
	exited ...<-chan int,
) error {
	job, ok := m.jobs[jid]
	if !ok {
		return fmt.Errorf("job %s does not exist. was it initialized?", jid)
	}

	job.SetDetails(&daemon.Details{PID: proto.Uint32(pid)})

	var exitedChan <-chan int
	if len(exited) == 0 {
		exitedChan = exited[0]
	} else {
		exitedChan = utils.WaitForPid(pid)
	}

	if job.GetProcess() == nil {
		process := &daemon.ProcessState{}
		err := utils.FillProcessState(ctx, pid, process)
		if err != nil {
			return fmt.Errorf("failed to fill process state: %w", err)
		}
		job.SetProcess(process)
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		<-exitedChan
		log.Info().Str("JID", jid).Uint32("PID", pid).Msg("job exited")
		job.SetRunning(false)

		gpuController, ok := m.gpuControllers[jid]
		if ok {
			gpuController.cmd.Process.Signal(syscall.SIGTERM)
		}
	}()

	return nil
}

func (m *ManagerDBLazy) Kill(jid string, signal ...syscall.Signal) error {
	job, ok := m.jobs[jid]
	if !ok {
		return fmt.Errorf("job %s does not exist", jid)
	}

	err := syscall.Kill(int(job.GetPID()), signal[0])
	if err != nil {
		return fmt.Errorf("failed to kill process: %w", err)
	}

	return nil
}

///////////////////////
//// GPU Management ///
///////////////////////

func (m *ManagerDBLazy) AttachGPU(
	ctx context.Context,
	wg *sync.WaitGroup,
	jid string,
	controller string,
) error {
	if _, err := os.Stat(controller); err != nil {
		return err
	}

	port, err := utils.GetFreePort()
	if err != nil {
		return err
	}

	cmd := exec.Command(controller, jid, "--port", strconv.Itoa(port))

	out := &bytes.Buffer{}
	cmd.Stdout = out
	cmd.Stderr = out
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Pdeathsig: syscall.SIGTERM,
	}

	err = cmd.Start()
	if err != nil {
		return fmt.Errorf(
			"failed to start GPU controller: %w",
			utils.GRPCErrorShort(err, out.String()),
		)
	}

	log.Debug().
		Str("JID", jid).
		Int("port", port).
		Msg("waiting for GPU controller...")

	opts := []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}
	conn, err := grpc.NewClient(fmt.Sprintf("%s:%d", GPU_CONTROLLER_HOST, port), opts...)
	if err != nil {
		cmd.Process.Signal(syscall.SIGTERM)
		return fmt.Errorf(
			"failed to create GPU controller client: %w",
			utils.GRPCErrorShort(err, out.String()),
		)
	}

	client := gpugrpc.NewControllerClient(conn)

	waitCtx, cancel := context.WithTimeout(ctx, GPU_CONTROLLER_WAIT_TIMEOUT)
	defer cancel()

	// Wait for early controller exit
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-utils.WaitForPidCtx(waitCtx, uint32(cmd.Process.Pid))
		cancel()
	}()

	resp, err := client.HealthCheck(waitCtx, &gpu.HealthCheckRequest{}, grpc.WaitForReady(true))
	if err != nil || !resp.Success {
		cmd.Process.Signal(syscall.SIGTERM)
		conn.Close()
		return fmt.Errorf(
			"failed to health check GPU controller: %w",
			utils.GRPCErrorShort(err, out.String()),
		)
	}

	log.Debug().Str("JID", jid).Msg("GPU controller ready")

	m.gpuControllers[jid] = &gpuController{
		cmd:    cmd,
		client: client,
		conn:   conn,
		out:    out,
	}

	// Cleanup controller on exit, and signal job of its exit
	wg.Add(1)
	go func() {
		defer wg.Done()

		err := cmd.Wait()
		if err != nil {
			log.Trace().Err(err).Str("JID", jid).Msg("GPU controller Wait()")
		}
		log.Debug().
			Int("code", cmd.ProcessState.ExitCode()).
			Str("JID", jid).
			Msg("GPU controller exited")

		m.Kill(jid, GPU_CONTROLLER_PREMATURE_EXIT_SIGNAL)
		conn.Close()
		delete(m.gpuControllers, jid)
	}()

	return nil
}

func (m *ManagerDBLazy) AttachGPUAsync(
	ctx context.Context,
	wg *sync.WaitGroup,
	jid string,
	controller string,
) <-chan error {
	err := make(chan error)

	wg.Add(1)
	go func() {
		defer wg.Done()
		defer close(err)
		for {
			select {
			case <-ctx.Done():
				return
			case err <- m.AttachGPU(ctx, wg, jid, controller):
				return
			}
		}
	}()

	return err
}

func (m *ManagerDBLazy) DumpGPU(ctx context.Context, jid string) error {
	return nil
}

func (m *ManagerDBLazy) DumpGPUAsync(ctx context.Context, jid string) <-chan error {
	return nil
}

func (m *ManagerDBLazy) RestoreGPU(ctx context.Context, jid string) error {
	return nil
}

func (m *ManagerDBLazy) RestoreGPUAsync(ctx context.Context, jid string) <-chan error {
	return nil
}
