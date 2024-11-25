package job

// Implements a lazy job manager, that uses the DB as a backing store.
// Since methods cannot fail, we manage state in-memory, keeping the DB in sync
// lazily in the background with retry logic.

import (
	"context"
	"fmt"
	"sync"
	"syscall"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/internal/db"
	"github.com/cedana/cedana/pkg/utils"
	"github.com/rs/zerolog/log"
	"google.golang.org/protobuf/proto"
)

type ManagerDBLazy struct {
	db   db.DB
	jobs map[string]*Job

	gpuControllers map[string]*gpuController
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
		// Attempt to fill process state, if process is still running
		process := &daemon.ProcessState{}
		err := utils.FillProcessState(ctx, pid, process)
		if err == nil {
			job.SetProcess(process)
		}
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

	if !job.IsRunning() {
		// We don't want to make a random syscall to kill a job that isn't running
		// to avoid an unnecessary syscall
		return fmt.Errorf("job %s is not running", jid)
	}

	if len(signal) == 0 {
		signal = append(signal, syscall.SIGKILL)
	}

	err := syscall.Kill(int(job.GetPID()), signal[0])
	if err != nil {
		return fmt.Errorf("failed to kill process: %w", err)
	}

	return nil
}
