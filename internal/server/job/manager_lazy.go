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
	"github.com/cedana/cedana/pkg/plugins"
	"github.com/cedana/cedana/pkg/utils"
	"github.com/rs/zerolog/log"
)

// Pluggable features
const featureKillSignal plugins.Feature[syscall.Signal] = "KillSignal"

const DB_SYNC_RETRY_INTERVAL = 1 * time.Second

type ManagerLazy struct {
	jobs           sync.Map
	gpuControllers sync.Map

	plugins plugins.Manager
	db      db.DB
	pending chan action

	wg *sync.WaitGroup // for all manger background routines
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

// NewManagerLazy creates a new lazy job manager, that uses a DB as a backing store.
func NewManagerLazy(ctx context.Context, serverWg *sync.WaitGroup, plugins plugins.Manager, db db.DB) (*ManagerLazy, error) {
	manager := &ManagerLazy{
		jobs:           sync.Map{},
		gpuControllers: sync.Map{},
		pending:        make(chan action, 64),
		plugins:        plugins,
		wg:             &sync.WaitGroup{},
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
				manager.pending <- action{shutdown, nil}
				for action := range manager.pending {
					if action.typ == shutdown {
						break
					}
					ctx := context.WithoutCancel(ctx)
					err := action.sync(ctx, db)
					if err != nil {
						errs = append(errs, err)
						failedActions = append(failedActions, action)
					}
				}
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
			case action := <-manager.pending:
				err := action.sync(ctx, db)
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

	job := newJob(jid, jobType)
	m.jobs.Store(jid, job)

	m.pending <- action{update, job}

	return job, nil
}

func (m *ManagerLazy) Get(jid string) *Job {
	job, ok := m.jobs.Load(jid)
	if !ok {
		return nil
	}

	return job.(*Job)
}

func (m *ManagerLazy) Delete(jid string) {
	job, ok := m.jobs.Load(jid)
	if !ok {
		return
	}
	m.jobs.Delete(jid)

	m.pending <- action{remove, job.(*Job)}
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
		jobs = append(jobs, job)
		return true
	})

	return jobs
}

func (m *ManagerLazy) Exists(jid string) bool {
	_, ok := m.jobs.Load(jid)
	return ok
}

func (m *ManagerLazy) Manage(ctx context.Context, jid string, pid uint32, exited ...<-chan int) error {
	if !m.Exists(jid) {
		return fmt.Errorf("job %s does not exist. was it initialized?", jid)
	}

	job := m.Get(jid)

	var exitedChan <-chan int
	if len(exited) == 0 {
		exitedChan = exited[0]
	} else {
		exitedChan = utils.WaitForPid(pid)
	}

	job.SetPID(pid)

	m.pending <- action{update, job}

	log.Info().Str("JID", jid).Str("type", job.GetType()).Uint32("PID", pid).Msg("managing job")

	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		<-exitedChan
		log.Info().Str("JID", jid).Str("type", job.GetType()).Uint32("PID", pid).Msg("job exited")

		gpuController := m.getGPUController(jid)
		if gpuController != nil {
			gpuController.cmd.Process.Signal(syscall.SIGTERM)
		}
		m.pending <- action{update, job}
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
	err := featureKillSignal.IfAvailable(func(plugin string, pluginSignal syscall.Signal) error {
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
	if err != nil {
		return err
	}

	if len(signal) > 0 {
		signalToUse = signal[0]
	}

	err = syscall.Kill(int(job.GetPID()), signalToUse)
	if err != nil {
		return fmt.Errorf("failed to kill process: %w", err)
	}

	return nil
}

////////////////////////
//// Helper Methods ////
////////////////////////

func (i actionType) String() string {
	return [...]string{"update", "remove", "shutdown"}[i]
}

func (a *action) sync(ctx context.Context, db db.DB) error {
	job := a.job
	typ := a.typ
	var err error
	switch typ {
	case update:
		err = db.PutJob(ctx, job.JID, job.GetProto())
	case remove:
		err = db.DeleteJob(ctx, job.JID)
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

		if job.GPUEnabled() {
			m.addCRIUCallbackGPU(ctx, job.JID)
		}
	}

	return nil
}

func (m *ManagerLazy) getGPUController(jid string) *gpuController {
	controller, ok := m.gpuControllers.Load(jid)
	if !ok {
		return nil
	}
	return controller.(*gpuController)
}
