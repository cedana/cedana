package job

// Implements a lazy job manager, that uses the DB as a backing store.
// Since methods cannot fail, we manage state in-memory, keeping the DB in sync
// lazily in the background with retry logic.

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"syscall"
	"time"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/internal/db"
	"github.com/cedana/cedana/pkg/utils"
	"github.com/rs/zerolog/log"
	"google.golang.org/protobuf/proto"
)

const DB_SYNC_RETRY_INTERVAL = 1 * time.Second

type ManagerLazy struct {
	jobs           map[string]*Job
	gpuControllers map[string]*gpuController

	db      db.DB
	pending chan action
	wg      *sync.WaitGroup // for all manger background routines
}

type actionType int

const (
	update actionType = iota
	remove
	shutdown
)

type action struct {
	typ actionType
	job *Job
}

func (i actionType) String() string {
	return [...]string{"update", "remove", "shutdown"}[i]
}

func (a *action) sync(ctx context.Context, db db.DB) error {
	job := a.job
	typ := a.typ
	var err error
	switch typ {
	case update:
		log.Error().Msg("UPDAAAAAAAAAAAAAAAAAAAAAAAATE")
		err = db.PutJob(ctx, job.JID, job.GetProto())
	case remove:
		log.Error().Msg("DELEEEEEEEEEEEEEEEEEEEEEEEEEEEETE")
		err = db.DeleteJob(ctx, job.JID)
	}
	return err
}

func NewManagerDBLazy(ctx context.Context, serverWg *sync.WaitGroup) (*ManagerLazy, error) {
	db, err := db.NewLocalDB(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create new local db: %w", err)
	}

	wg := &sync.WaitGroup{}
	jobs := make(map[string]*Job)
	pending := make(chan action, 64)

	// First load all jobs from the DB
	protos, err := db.ListJobs(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to load jobs from db: %w", err)
	}
	for _, proto := range protos {
		job := fromProto(proto)
		jobs[job.JID] = job
	}

	// Spawn a background routine that will keep the DB in sync
	// with retry logic. Can extend to use a backoff strategy.
	serverWg.Add(1)
	go func() {
		defer serverWg.Done()
		for {
			select {
			case <-ctx.Done():
				log.Info().Msg("syncing DB before shutdown")
				var errs []error
				var failedActions []action
				wg.Wait() // wait for all background routines
				pending <- action{shutdown, nil}
				for action := range pending {
					if action.typ == shutdown {
						break
					}
					err := action.sync(ctx, db)
					if err != nil {
						errs = append(errs, err)
						failedActions = append(failedActions, action)
					}
				}
				close(pending)
				err = errors.Join(errs...)
				if err != nil {
					log.Error().Err(err).Msg("failed to sync DB before shutdown")
					for _, action := range failedActions {
						log.Debug().Str("JID", action.job.JID).
							Interface("job", *action.job.GetProto()).
							Msgf("failed %s", action.typ)
					}
				}
				return
			case action := <-pending:
				err := action.sync(ctx, db)
				if err != nil {
					pending <- action
					log.Debug().Err(err).Msg("DB sync failed, retrying in background")
					time.Sleep(DB_SYNC_RETRY_INTERVAL)
				}
			}
		}
	}()

	return &ManagerLazy{
		jobs:           jobs,
		gpuControllers: make(map[string]*gpuController),
		db:             db,
		pending:        pending,
		wg:             wg,
	}, nil
}

/////////////////
//// Methods ////
/////////////////

func (m *ManagerLazy) GetWG() *sync.WaitGroup {
	return m.wg
}

func (m *ManagerLazy) New(jid string, jobType string) (*Job, error) {
	if jid == "" {
		return nil, fmt.Errorf("missing JID")
	}

	job := newJob(jid, jobType)
	m.jobs[jid] = job

	m.pending <- action{update, job}

	return job, nil
}

func (m *ManagerLazy) Get(jid string) *Job {
	job, ok := m.jobs[jid]
	if !ok {
		return nil
	}

	return job
}

func (m *ManagerLazy) Delete(jid string) {
	job, ok := m.jobs[jid]
	if !ok {
		return
	}
	delete(m.jobs, jid)

	m.pending <- action{remove, job}
}

func (m *ManagerLazy) List(jids ...string) []*Job {
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

func (m *ManagerLazy) Exists(jid string) bool {
	_, ok := m.jobs[jid]
	return ok
}

func (m *ManagerLazy) Manage(
	ctx context.Context,
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
			m.pending <- action{update, job}
		}
	}

	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		<-exitedChan
		log.Info().Str("JID", jid).Uint32("PID", pid).Msg("job exited")
		job.SetRunning(false)

		gpuController, ok := m.gpuControllers[jid]
		if ok {
			gpuController.cmd.Process.Signal(syscall.SIGTERM)
		}
		m.pending <- action{update, job}
	}()

	return nil
}

func (m *ManagerLazy) Kill(jid string, signal ...syscall.Signal) error {
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
