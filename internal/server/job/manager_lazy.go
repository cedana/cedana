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
	"github.com/cedana/cedana/internal/server/gpu"
	"github.com/cedana/cedana/pkg/criu"
	"github.com/cedana/cedana/pkg/features"
	"github.com/cedana/cedana/pkg/plugins"
	"github.com/cedana/cedana/pkg/utils"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

const DB_SYNC_RETRY_INTERVAL = 1 * time.Second

type ManagerLazy struct {
	jobs        sync.Map
	checkpoints sync.Map

	plugins plugins.Manager
	gpus    gpu.Manager
	db      db.DB
	pending chan action

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

	err := manager.syncWithDB(lifetime, action{initialize, ""}, db)
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
				log.Info().Msg("syncing DB before shutdown")
				var errs []error
				var failedActions []action
				manager.wg.Wait() // wait for all background routines
				manager.pending <- action{shutdown, ""}
				for action := range manager.pending {
					if action.typ == shutdown {
						break
					}
					ctx := context.WithoutCancel(lifetime)
					err := manager.syncWithDB(ctx, action, db)
					if err != nil {
						errs = append(errs, err)
						failedActions = append(failedActions, action)
					}
				}
				err = errors.Join(errs...)
				if err != nil {
					log.Error().Msg("failed to sync DB before shutdown")
					for i, action := range failedActions {
						log.Debug().Err(errs[i]).Str("id", action.id).Str("type", action.typ.String()).Send()
					}
				}
				return
			case action := <-manager.pending:
				err := manager.syncWithDB(lifetime, action, db)
				if err != nil {
					manager.pending <- action
					log.Debug().Err(err).Str("id", action.id).Str("type", action.typ.String()).Msg("DB sync failed, retrying...")
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

	return job, nil
}

func (m *ManagerLazy) Get(jid string) *Job {
	job, ok := m.jobs.Load(jid)
	if !ok {
		return nil
	}

	if !job.(*Job).GPUEnabled() {
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
		if !job.GPUEnabled() {
			job.SetGPUEnabled(m.gpus.IsAttached(jid))
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
		if !job.GPUEnabled() {
			job.SetGPUEnabled(m.gpus.IsAttached(job.JID))
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
	// Only possible if process is still running. For short lived process, manage is not useful
	// anyways, so we just fail here.
	err := job.FillState(lifetime, pid)
	if err != nil {
		return fmt.Errorf("failed to fill process state after manage (process exited early?): %w", err)
	}

	m.pending <- action{putJob, jid}

	log.Info().Str("JID", jid).Str("type", job.GetType()).Uint32("PID", pid).Msg("managing job")

	m.wg.Add(1)
	go func() {
		defer m.wg.Done()

		select {
		case <-lifetime.Done():
		case <-exitedChan:
		}

		log.Info().Str("JID", jid).Str("type", job.GetType()).Uint32("PID", pid).Msg("job exited")

		m.gpus.Detach(jid)

		// TODO: separate checkpoint DB which also avoids race b/w job exit and checkpoint addition
		// if !config.Global.DB.LeaveDead {
		// 	// Delete the job if no checkpoints were made as it's good for nothing
		// 	if len(job.GetCheckpoints()) == 0 {
		// 		m.Delete(jid)
		// 	}
		// }

		m.pending <- action{putJob, jid}
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

func (m *ManagerLazy) AddCheckpoint(jid string, path string) {
	job := m.Get(jid)
	if job == nil {
		return
	}

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
	_, ok := m.checkpoints.Load(id)
	if !ok {
		return
	}
	m.checkpoints.Delete(id)

	m.pending <- action{putCheckpoint, id}
}

func (m *ManagerLazy) CRIUCallback(lifetime context.Context, jid string) *criu.NotifyCallbackMulti {
	job := m.Get(jid)
	if job == nil {
		return nil
	}
	multiCallback := &criu.NotifyCallbackMulti{}
	multiCallback.IncludeMulti(job.GetCRIUCallback())
	if job.GPUEnabled() {
		multiCallback.Include(m.gpus.CRIUCallback(jid))
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
	return [...]string{"init", "putJob", "putCheckpoint", "shutdown"}[i]
}

func (m *ManagerLazy) syncWithDB(ctx context.Context, action action, db db.DB) error {
	typ := action.typ

	var err error
	switch typ {
	case initialize:
		jobProtos, err := db.ListJobs(ctx)
		if err != nil {
			break
		}
		for _, proto := range jobProtos {
			job := fromProto(proto)
			m.jobs.Store(job.JID, job)

			checkpoints, err := db.ListCheckpointsByJIDs(ctx, job.JID)
			if err != nil {
				break
			}
			for _, checkpoint := range checkpoints {
				m.checkpoints.Store(checkpoint.ID, checkpoint)
			}
		}
	case putJob:
		jid := action.id
		job := m.Get(jid)
		if job == nil {
			err = db.DeleteJob(ctx, jid)
		} else {
			err = db.PutJob(ctx, job.GetProto())
		}
	case putCheckpoint:
		id := action.id
		checkpoint, ok := m.checkpoints.Load(id)
		if !ok {
			err = db.DeleteCheckpoint(ctx, id)
		} else {
			err = db.PutCheckpoint(ctx, checkpoint.(*daemon.Checkpoint))
		}
	}
	return err
}
