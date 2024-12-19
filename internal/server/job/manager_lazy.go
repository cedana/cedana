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

	"github.com/cedana/cedana/internal/db"
	"github.com/cedana/cedana/internal/features"
	"github.com/cedana/cedana/internal/server/gpu"
	"github.com/cedana/cedana/pkg/criu"
	"github.com/cedana/cedana/pkg/plugins"
	"github.com/cedana/cedana/pkg/utils"
	"github.com/rs/zerolog/log"
)

const DB_SYNC_RETRY_INTERVAL = 1 * time.Second

type ManagerLazy struct {
	jobs sync.Map

	plugins plugins.Manager
	gpus    gpu.Manager
	db      db.DB
	pending chan action

	wg *sync.WaitGroup // for all manger background routines
}

type actionType int

const (
	update actionType = iota
	shutdown
)

type action struct {
	typ actionType
	JID string
}

// NewManagerLazy creates a new lazy job manager, that uses a DB as a backing store.
func NewManagerLazy(
	ctx context.Context,
	serverWg *sync.WaitGroup,
	plugins plugins.Manager,
	gpus gpu.Manager,
	db db.DB,
) (*ManagerLazy, error) {
	gpuManager := gpus
	if gpuManager == nil {
		gpuManager = gpu.ManagerMissing{}
	}

	manager := &ManagerLazy{
		jobs:    sync.Map{},
		pending: make(chan action, 64),
		wg:      &sync.WaitGroup{},
		plugins: plugins,
		gpus:    gpuManager,
	}

	// Reload all jobs from the DB
	err := manager.loadFromDB(ctx, db)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize DB: %w", err)
	}
	manager.db = db

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
				manager.wg.Wait() // wait for all background routines
				manager.pending <- action{shutdown, ""}
				for action := range manager.pending {
					if action.typ == shutdown {
						break
					}
					ctx := context.WithoutCancel(ctx)
					err := manager.sync(ctx, action, db)
					if err != nil {
						errs = append(errs, err)
						failedActions = append(failedActions, action)
					}
				}
				err = errors.Join(errs...)
				if err != nil {
					log.Error().Err(err).Msg("failed to sync DB before shutdown")
					for _, action := range failedActions {
						log.Debug().Str("JID", action.JID).
							Msgf("failed %s", action.typ)
					}
				}
				return
			case action := <-manager.pending:
				err := manager.sync(ctx, action, db)
				if err != nil {
					manager.pending <- action
					log.Debug().Err(err).Msg("DB sync failed, retrying in background")
					time.Sleep(DB_SYNC_RETRY_INTERVAL)
				}
			}
		}
	}()

	return manager, nil
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

	if m.Exists(jid) {
		return nil, fmt.Errorf("job %s already exists", jid)
	}

	job := newJob(jid, jobType)
	m.jobs.Store(jid, job)

	m.pending <- action{update, jid}

	return job, nil
}

func (m *ManagerLazy) Get(jid string) *Job {
	job, ok := m.jobs.Load(jid)
	if !ok {
		return nil
	}

	if !job.(*Job).GPUEnabled() { // FIXME: need a better way
		job.(*Job).SetGPUEnabled(m.gpus.IsAttached(jid))
	}

	return job.(*Job)
}

func (m *ManagerLazy) Find(pid uint32) *Job {
	var found *Job
	m.jobs.Range(func(key any, val any) bool {
		job := val.(*Job)
		if job.GetPID() == pid {
			found = job
			return false
		}
		return true
	})
	return found
}

func (m *ManagerLazy) Delete(jid string) {
	_, ok := m.jobs.Load(jid)
	if !ok {
		return
	}
	m.jobs.Delete(jid)

	m.pending <- action{update, jid}
}

func (m *ManagerLazy) List(jids ...string) []*Job {
	var jobs []*Job

	jidSet := make(map[string]any)
	for _, jid := range jids {
		jidSet[jid] = nil
	}

	m.jobs.Range(func(key any, val any) bool {
		jid := key.(string)
		job := val.(*Job)
		if _, ok := jidSet[jid]; len(jids) > 0 && !ok {
			return true
		}
		if !job.GPUEnabled() { // FIXME: need a better way
			job.SetGPUEnabled(m.gpus.IsAttached(jid))
		}
		jobs = append(jobs, job)
		return true
	})

	return jobs
}

func (m *ManagerLazy) Exists(jid string) bool {
	_, ok := m.jobs.Load(jid)
	return ok
}

func (m *ManagerLazy) Manage(lifetime context.Context, jid string, pid uint32, exited ...<-chan int) error {
	if !m.Exists(jid) {
		return fmt.Errorf("job %s does not exist. was it initialized?", jid)
	}

	job := m.Get(jid)

	var exitedChan <-chan int
	if len(exited) > 0 {
		exitedChan = exited[0]
	} else {
		exitedChan = utils.WaitForPid(pid)
	}

	// Try to update the process state with the latest information,
	// Only possible if process is still running, otherwise ignore errors.
	err := job.FillProcess(lifetime, pid)
	if err != nil {
		log.Warn().Err(err).Str("JID", jid).Str("type", job.GetType()).Uint32("PID", pid).Msg("ignoring: failed to fill process state after manage")
	}

	m.pending <- action{update, jid}

	log.Info().Str("JID", jid).Str("type", job.GetType()).Uint32("PID", pid).Msg("managing job")

	m.wg.Add(1)
	go func() {
		defer m.wg.Done()

		select {
		case <-lifetime.Done():
		case <-exitedChan:
		}

		log.Info().Str("JID", jid).Str("type", job.GetType()).Uint32("PID", pid).Msg("job exited")

		m.gpus.Detach(lifetime, jid)

		m.pending <- action{update, jid}
	}()

	return nil
}

func (m *ManagerLazy) Kill(jid string, signal ...syscall.Signal) error {
	if !m.Exists(jid) {
		return fmt.Errorf("job %s does not exist", jid)
	}

	job := m.Get(jid)

	if !job.IsRunning() {
		// We don't want to make a random syscall to kill a job that isn't running
		// to avoid an unnecessary syscall
		return fmt.Errorf("job %s is not running", jid)
	}

	signalToUse := syscall.SIGKILL

	// Check if the plugin for the job type exports a custom signal
	features.KillSignal.IfAvailable(func(plugin string, pluginSignal syscall.Signal) error {
		if len(signal) > 0 {
			return fmt.Errorf(
				"%s plugin exports a custom kill signal `%s`, so cannot use signal `%s`",
				plugin, pluginSignal, signal[0])
		}
		log.Debug().
			Str("JID", job.JID).
			Str("type", job.GetType()).
			Str("signal", pluginSignal.String()).
			Msg("using custom kill signal exported by plugin")
		signalToUse = pluginSignal
		return nil
	}, job.GetType())

	if len(signal) > 0 {
		signalToUse = signal[0]
	}

	err := syscall.Kill(int(job.GetPID()), signalToUse)
	if err != nil {
		return fmt.Errorf("failed to kill process: %w", err)
	}

	return nil
}

func (m *ManagerLazy) CRIUCallback(lifetime context.Context, jid string) *criu.NotifyCallbackMulti {
	job := m.Get(jid)
	if job == nil {
		return nil
	}
	multiCallback := &criu.NotifyCallbackMulti{}
	multiCallback.IncludeMulti(job.GetCRIUCallback())
	if job.GPUEnabled() {
		multiCallback.Include(m.gpus.CRIUCallback(lifetime, jid))
	}
	return multiCallback
}

func (m *ManagerLazy) GPUs() gpu.Manager {
	return m.gpus
}

////////////////////////
//// Helper Methods ////
////////////////////////

func (i actionType) String() string {
	return [...]string{"update", "remove", "shutdown"}[i]
}

func (m *ManagerLazy) sync(ctx context.Context, action action, db db.DB) error {
	jid := action.JID
	typ := action.typ

	job := m.Get(jid)

	var err error
	if typ == update {
		if job == nil {
			err = db.DeleteJob(ctx, jid)
		} else {
			err = db.PutJob(ctx, jid, job.GetProto())
		}
	}
	return err
}

func (m *ManagerLazy) loadFromDB(ctx context.Context, db db.DB) error {
	protos, err := db.ListJobs(ctx)
	if err != nil {
		return fmt.Errorf("failed to list jobs: %w", err)
	}
	for _, proto := range protos {
		job := fromProto(proto)
		m.jobs.Store(job.JID, job)
	}

	return nil
}
