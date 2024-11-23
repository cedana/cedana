package job

// Implements a job manager, that uses the DB as a backing store.
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
	"github.com/rb-go/namegen"
	"github.com/rs/zerolog/log"
	"google.golang.org/protobuf/proto"
)

const DEFAULT_LOG_PATH_FORMATTER string = "/var/log/cedana-output-%s.log"

type ManagerDBLazy struct {
	db   db.DB
	jobs map[string]*Job
}

func NewManagerDBLazy(ctx context.Context, wg *sync.WaitGroup) (*ManagerDBLazy, error) {
	db, err := db.NewLocalDB(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create new local db: %w", err)
	}

	return &ManagerDBLazy{db, make(map[string]*Job)}, nil
}

func (m *ManagerDBLazy) New(jid string, jobType string, log ...string) (*Job, error) {
	if jid != "" {
		if m.Exists(jid) {
			return nil, fmt.Errorf("job %s already exists", jid)
		}
	} else {
		jid = NewJID()
	}

	job := newJob(jid, jobType)
	m.jobs[jid] = job

	var logPath string
	if len(log) == 0 || log[0] == "" {
		logPath = fmt.Sprintf(DEFAULT_LOG_PATH_FORMATTER, jid)
	} else {
		logPath = log[0]
	}
	job.SetLog(logPath)

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
	job, ok := m.jobs[jid]
	if !ok {
		return
	}
	job.Wait()
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

	if len(exited) == 0 {
		job.SetExited(exited[0])
	} else {
		job.SetExited(utils.WaitForPid(pid))
	}

	process := &daemon.ProcessState{}
	err := utils.FillProcessState(ctx, pid, process)
	if err != nil {
		return fmt.Errorf("failed to fill process state: %w", err)
	}
	job.SetProcess(process)

	wg.Add(1)
	go func() {
		defer wg.Done()
		job.Wait()
		log.Info().Str("JID", jid).Uint32("PID", pid).Msg("job exited")
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

/////////////////
//// Helpers ////
/////////////////

func NewJID() string {
	return namegen.GetName(1)
}
