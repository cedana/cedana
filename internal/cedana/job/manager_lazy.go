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
	"github.com/cedana/cedana/internal/cedana/gpu"
	"github.com/cedana/cedana/internal/db"
	"github.com/cedana/cedana/pkg/features"
	"github.com/cedana/cedana/pkg/plugins"
	"github.com/cedana/cedana/pkg/utils"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

const (
	DB_SYNC_INTERVAL       = 10 * time.Second
	DB_SYNC_RETRY_INTERVAL = 1 * time.Second
)

type ManagerLazy struct {
	jobs               sync.Map
	checkpoints        sync.Map
	deletedJobs        sync.Map // to keep track of deleted jobs
	deletedCheckpoints sync.Map // to keep track of deleted checkpoints

	host    *daemon.Host
	plugins plugins.Manager
	gpus    gpu.Manager
	db      db.DB
	pending chan action
	sync    sync.Mutex // to protect syncWithDB from concurrent access

	wg *sync.WaitGroup // for all manger background routines
}

type actionType int

const (
	initialize actionType = iota
	putJob
	putCheckpoint
	shutdown
)

type action struct {
	typ actionType
	id  string
}

// NewManagerLazy creates a new lazy job manager, that uses a DB as a backing store.
func NewManagerLazy(
	lifetime context.Context,
	serverWg *sync.WaitGroup,
	host *daemon.Host,
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
		host:    host,
		plugins: plugins,
		gpus:    gpuManager,
		db:      db,
	}

	err := manager.Sync(lifetime)
	if err != nil {
		return nil, err
	}

	// Spawn a background routine that will keep the DB in sync
	// with retry logic. Can extend to use a backoff strategy.
	serverWg.Add(1)
	go func() {
		defer serverWg.Done()
		for {
			select {
			case <-lifetime.Done():
				log.Info().Msg("syncing job manager with DB before shutdown")
				var errs []error
				var failedActions []action
				manager.wg.Wait() // wait for all background routines
				manager.pending <- action{shutdown, ""}
				for action := range manager.pending {
					if action.typ == shutdown {
						break
					}
					ctx := context.WithoutCancel(lifetime)
					err := manager.syncWithDB(ctx, action)
					if err != nil {
						errs = append(errs, err)
						failedActions = append(failedActions, action)
					}
				}
				err = errors.Join(errs...)
				if err != nil {
					log.Error().Msg("failed to sync job manager with DB before shutdown")
					for i, action := range failedActions {
						log.Debug().Err(errs[i]).Str("id", action.id).Str("type", action.typ.String()).Send()
					}
				}
				return
			case action := <-manager.pending:
				err := manager.syncWithDB(lifetime, action)
				if err != nil {
					manager.pending <- action
					log.Debug().Err(err).Str("id", action.id).Str("type", action.typ.String()).Msg("job manager DB sync failed, retrying...")
					time.Sleep(DB_SYNC_RETRY_INTERVAL)
				}
			case <-time.After(DB_SYNC_INTERVAL):
				manager.pending <- action{initialize, ""} // periodically sync with DB
			}
		}
	}()

	return manager, nil
}

/////////////////
//// Methods ////
/////////////////

func (m *ManagerLazy) New(jid string, jobType string) (*Job, error) {
	if jid == "" {
		return nil, fmt.Errorf("missing JID")
	}

	if m.Exists(jid) {
		return nil, fmt.Errorf("job %s already exists", jid)
	}

	job := newJob(jid, jobType, m.host)
	m.jobs.Store(jid, job)

	return job, nil
}

func (m *ManagerLazy) Get(jid string) *Job {
	job, ok := m.jobs.Load(jid)
	if !ok {
		return nil
	}

	job.(*Job).SyncDeep()

	if !job.(*Job).GPUEnabled() {
		job.(*Job).SetGPUEnabled(m.gpus.IsAttached(job.(*Job).GetPID()))
	}

	return job.(*Job)
}

func (m *ManagerLazy) Delete(jid string) {
	job, ok := m.jobs.Load(jid)
	if !ok {
		return
	}
	m.jobs.Delete(jid)

	m.deletedJobs.Store(jid, job)

	m.pending <- action{putJob, jid}

	checkpoints := m.ListCheckpoints(jid)
	for _, checkpoint := range checkpoints {
		m.DeleteCheckpoint(checkpoint.ID)
	}
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
		job.Sync()
		if !job.GPUEnabled() {
			job.SetGPUEnabled(m.gpus.IsAttached(job.GetPID()))
		}
		jobs = append(jobs, job)
		return true
	})

	return jobs
}

func (m *ManagerLazy) ListByHostIDs(hostIDs ...string) []*Job {
	var jobs []*Job

	hostIDSet := make(map[string]any)
	for _, hostID := range hostIDs {
		hostIDSet[hostID] = nil
	}

	m.jobs.Range(func(key any, val any) bool {
		job := val.(*Job)
		hostID := job.GetState().GetHost().GetID()
		if _, ok := hostIDSet[hostID]; len(hostIDs) > 0 && !ok {
			return true
		}
		job.Sync()
		if !job.GPUEnabled() {
			job.SetGPUEnabled(m.gpus.IsAttached(job.GetPID()))
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

func (m *ManagerLazy) Manage(lifetime context.Context, jid string, pid uint32, code <-chan int) error {
	if !m.Exists(jid) {
		return fmt.Errorf("job %s does not exist. was it initialized?", jid)
	}

	job := m.Get(jid)
	job.SetPID(pid)
	job.SyncDeep()

	m.pending <- action{putJob, jid}

	log.Info().Str("JID", jid).Str("type", job.GetType()).Uint32("PID", pid).Msg("managing job")

	m.wg.Add(1)
	go func() {
		defer m.wg.Done()

		var exitCode int

		select {
		case <-lifetime.Done():
			m.Kill(jid)
			exitCode = <-code
		case exitCode = <-code:
		}

		log.Info().Str("JID", jid).Str("type", job.GetType()).Int("code", exitCode).Uint32("PID", pid).Msg("job exited")

		m.gpus.Detach(lifetime, pid)

		// Check if a clean up handler is available for the job type

		features.Cleanup.IfAvailable(func(_ string, cleanup func(*daemon.Details) error) error {
			log.Debug().Str("JID", jid).Str("type", job.GetType()).Msg("using custom cleanup from plugin")
			return cleanup(job.GetDetails())
		}, job.GetType())
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
		log.Debug().
			Str("JID", job.JID).
			Str("type", job.GetType()).
			Str("signal", pluginSignal.String()).
			Msg("using custom kill signal exported by plugin as the default")
		signalToUse = pluginSignal
		return nil
	}, job.GetType())

	if len(signal) > 0 {
		signalToUse = signal[0]
	}

	pid := job.GetPID()

	if pid != 0 {
		pgid := job.GetPGID()
		target := int(job.GetPID())
		if pgid == pid {
			target = -int(pgid) // If the job is the process group leader, send signal to the entire process group
		}

		return syscall.Kill(target, signalToUse)
	}

	return fmt.Errorf("job %s is not running, PID %d is not valid", jid, pid)
}

func (m *ManagerLazy) AddCheckpoint(jid string, paths []string) {
	job := m.Get(jid)
	if job == nil {
		return
	}

	for _, path := range paths {
		size, _ := utils.SizeFromPath(path)
		checkpoint := &daemon.Checkpoint{
			ID:   uuid.New().String(),
			JID:  jid,
			Path: path,
			Time: time.Now().UnixMilli(),
			Size: size,
		}
		m.checkpoints.Store(checkpoint.ID, checkpoint)

		m.pending <- action{putCheckpoint, checkpoint.ID}
	}
}

func (m *ManagerLazy) GetCheckpoint(id string) *daemon.Checkpoint {
	checkpoint, ok := m.checkpoints.Load(id)
	if !ok {
		return nil
	}
	return checkpoint.(*daemon.Checkpoint)
}

func (m *ManagerLazy) ListCheckpoints(jid string) []*daemon.Checkpoint {
	var checkpoints []*daemon.Checkpoint

	m.checkpoints.Range(func(key any, val any) bool {
		checkpoint := val.(*daemon.Checkpoint)
		if checkpoint.JID == jid {
			checkpoints = append(checkpoints, checkpoint)
		}
		return true
	})

	return checkpoints
}

func (m *ManagerLazy) GetLatestCheckpoint(jid string) *daemon.Checkpoint {
	var latest *daemon.Checkpoint

	m.checkpoints.Range(func(key any, val any) bool {
		checkpoint := val.(*daemon.Checkpoint)
		if checkpoint.JID == jid {
			if latest == nil || checkpoint.Time > latest.Time {
				latest = checkpoint
			}
		}
		return true
	})

	return latest
}

func (m *ManagerLazy) DeleteCheckpoint(id string) {
	c, ok := m.checkpoints.Load(id)
	if !ok {
		return
	}
	m.checkpoints.Delete(id)

	m.deletedCheckpoints.Store(id, c)

	m.pending <- action{putCheckpoint, id}
}

func (m *ManagerLazy) Sync(ctx context.Context) error {
	return m.syncWithDB(ctx, action{initialize, ""})
}

////////////////////////
//// Helper Methods ////
////////////////////////

func (i actionType) String() string {
	return [...]string{"init", "putJob", "putCheckpoint", "shutdown"}[i]
}

func (m *ManagerLazy) syncWithDB(ctx context.Context, action action) error {
	m.sync.Lock()
	defer m.sync.Unlock()
	typ := action.typ

	var err error

	switch typ {
	case initialize:
		jobProtos, err := m.db.ListJobs(ctx)
		if err != nil {
			return err
		}
		for _, proto := range jobProtos {
			job := fromProto(proto)
			_, deleted := m.deletedJobs.Load(job.JID)

			if !deleted && (!m.Exists(job.JID) || job.IsRemote()) {
				m.jobs.Store(job.JID, job)

				checkpoints, err := m.db.ListCheckpointsByJIDs(ctx, job.JID)
				if err != nil {
					return err
				}

				for _, checkpoint := range checkpoints {
					m.checkpoints.Store(checkpoint.ID, checkpoint)
				}
			}
		}

		// TODO: Can also remove stale jobs from memory. But need to be careful
		// about race conditions. For now, we just keep them in memory until daemon
		// is restarted.

	case putJob:
		jid := action.id
		job, ok := m.jobs.Load(jid)
		if ok {
			err = m.db.PutJob(ctx, job.(*Job).GetProto())
		} else if _, deleted := m.deletedJobs.Load(jid); deleted {
			err = m.db.DeleteJob(ctx, jid)
			if err == nil {
				m.deletedJobs.Delete(jid)
			}
		}
	case putCheckpoint:
		id := action.id
		checkpoint, ok := m.checkpoints.Load(id)
		if ok {
			err = m.db.PutCheckpoint(ctx, checkpoint.(*daemon.Checkpoint))
		} else if _, deleted := m.deletedCheckpoints.Load(id); deleted {
			err = m.db.DeleteCheckpoint(ctx, id)
			if err == nil {
				m.deletedCheckpoints.Delete(id)
			}
		}
	}
	return err
}
