package job

import (
	"context"
	"sync"
	"testing"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/internal/db"
)

type mockDB struct {
	db.DB
	jobs        map[string]*daemon.Job
	checkpoints map[string]*daemon.Checkpoint
	mu          sync.RWMutex
}

func newMockDB() *mockDB {
	return &mockDB{
		jobs:        make(map[string]*daemon.Job),
		checkpoints: make(map[string]*daemon.Checkpoint),
	}
}

func (m *mockDB) ListJobs(ctx context.Context, jids ...string) ([]*daemon.Job, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var jobs []*daemon.Job

	if len(jids) > 0 {
		jidMap := make(map[string]bool)
		for _, j := range jids {
			jidMap[j] = true
		}
		for _, j := range m.jobs {
			if jidMap[j.JID] {
				jobs = append(jobs, j)
			}
		}
		return jobs, nil
	}

	for _, j := range m.jobs {
		jobs = append(jobs, j)
	}
	return jobs, nil
}

func (m *mockDB) PutJob(ctx context.Context, job *daemon.Job) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.jobs[job.JID] = job
	return nil
}

func (m *mockDB) DeleteJob(ctx context.Context, jid string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.jobs, jid)
	return nil
}

func (m *mockDB) ListCheckpointsByJIDs(ctx context.Context, jids ...string) ([]*daemon.Checkpoint, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var cps []*daemon.Checkpoint
	jidMap := make(map[string]bool)
	for _, j := range jids {
		jidMap[j] = true
	}
	for _, c := range m.checkpoints {
		if jidMap[c.JID] {
			cps = append(cps, c)
		}
	}
	return cps, nil
}

func (m *mockDB) PutCheckpoint(ctx context.Context, checkpoint *daemon.Checkpoint) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.checkpoints[checkpoint.ID] = checkpoint
	return nil
}

func (m *mockDB) DeleteCheckpoint(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.checkpoints, id)
	return nil
}

func TestManagerLazy_Lifecycle(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	wg := &sync.WaitGroup{}
	db := newMockDB()

	m, err := NewManagerLazy(ctx, wg, nil, nil, nil, db)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}

	jid := "test-job-1"

	job, err := m.New(jid, "container")
	if err != nil {
		t.Fatalf("failed to create job: %v", err)
	}
	if job == nil {
		t.Fatal("expected non-nil job")
	}
	if !m.Exists(jid) {
		t.Fatal("expected manager to report job exists")
	}
	if _, ok := m.unpersistedJobs.Load(jid); !ok {
		t.Fatal("expected new job to be marked as unpersisted")
	}

	act := <-m.pending
	if act.typ != putJob || act.id != jid {
		t.Fatalf("expected pending action putJob for %s, got %v", jid, act)
	}

	retrievedJob := m.Get(ctx, jid)
	if retrievedJob == nil || retrievedJob.JID != jid {
		t.Fatalf("failed to get job %s", jid)
	}

	m.Delete(jid)
	if m.Exists(jid) {
		t.Fatal("expected manager to report job no longer exists after delete")
	}

	if _, ok := m.deletedJobs.Load(jid); !ok {
		t.Fatal("expected job to be in deletedJobs map")
	}

	if _, ok := m.unpersistedJobs.Load(jid); ok {
		t.Fatal("expected job to be removed from unpersistedJobs map upon deletion")
	}
}

func TestManagerLazy_SyncWithDB_Pruning(t *testing.T) {
	ctx := context.Background()
	mockdb := newMockDB()

	m := &ManagerLazy{
		jobs:               sync.Map{},
		checkpoints:        sync.Map{},
		deletedJobs:        sync.Map{},
		deletedCheckpoints: sync.Map{},
		unpersistedJobs:    sync.Map{},
		pending:            make(chan action, 64),
		db:                 mockdb,
		sync:               sync.Mutex{},
	}

	jidPersisted := "job-persisted"
	_, _ = m.New(jidPersisted, "process")
	mockdb.PutJob(ctx, &daemon.Job{JID: jidPersisted})
	m.unpersistedJobs.Delete(jidPersisted)

	jidUnpersisted := "job-unpersisted"
	_, _ = m.New(jidUnpersisted, "process")

	jidDeletedLocally := "job-deleted-locally"
	_, _ = m.New(jidDeletedLocally, "process")
	m.Delete(jidDeletedLocally)

	jidStale := "job-stale"
	_, _ = m.New(jidStale, "process")
	m.unpersistedJobs.Delete(jidStale)

	jidRunning := "job-running"
	jobRunning := fromProto(&daemon.Job{
		JID:  jidRunning,
		Type: "process",
		State: &daemon.ProcessState{
			IsRunning: true,
			Status:    "running",
		},
	})
	m.jobs.Store(jidRunning, jobRunning)

	cidPersisted := "chk-persisted"
	cpPersisted := &daemon.Checkpoint{ID: cidPersisted, JID: jidPersisted}
	m.checkpoints.Store(cidPersisted, cpPersisted)
	mockdb.PutCheckpoint(ctx, cpPersisted)

	cidStale := "chk-stale"
	m.checkpoints.Store(cidStale, &daemon.Checkpoint{
		ID:  cidStale,
		JID: jidStale,
	})

	for len(m.pending) > 0 {
		<-m.pending
	}

	err := m.syncWithDB(ctx, action{typ: initialize, id: ""})
	if err != nil {
		t.Fatalf("syncWithDB failed: %v", err)
	}

	if !m.Exists(jidPersisted) {
		t.Errorf("expected persisted job %s to exist", jidPersisted)
	}

	if !m.Exists(jidUnpersisted) {
		t.Errorf("expected newly created unpersisted job %s to be protected from pruning", jidUnpersisted)
	}

	if m.Exists(jidDeletedLocally) {
		t.Errorf("expected deleted job %s to not exist in main map", jidDeletedLocally)
	}
	if _, ok := m.deletedJobs.Load(jidDeletedLocally); !ok {
		t.Errorf("expected locally deleted job %s to still be queued for deletion sync", jidDeletedLocally)
	}

	if m.Exists(jidStale) {
		t.Errorf("expected stale job %s (not in DB, not unpersisted) to be pruned from memory", jidStale)
	}

	if !m.Exists(jidRunning) {
		t.Errorf("expected running job %s to be protected from pruning", jidRunning)
	}

	if _, ok := m.checkpoints.Load(cidPersisted); !ok {
		t.Errorf("expected persisted checkpoint %s to exist", cidPersisted)
	}

	if _, ok := m.checkpoints.Load(cidStale); ok {
		t.Errorf("expected stale checkpoint %s to be pruned from memory", cidStale)
	}
}

func TestManagerLazy_SyncWithDB_PutJob(t *testing.T) {
	ctx := context.Background()
	mockdb := newMockDB()

	m := &ManagerLazy{
		jobs:            sync.Map{},
		deletedJobs:     sync.Map{},
		unpersistedJobs: sync.Map{},
		db:              mockdb,
		pending:         make(chan action, 64),
		sync:            sync.Mutex{},
	}

	jid := "sync-test-job"
	_, _ = m.New(jid, "process")

	<-m.pending

	err := m.syncWithDB(ctx, action{typ: putJob, id: jid})
	if err != nil {
		t.Fatalf("syncWithDB putJob failed: %v", err)
	}

	dbJobs, _ := mockdb.ListJobs(ctx)
	if len(dbJobs) != 1 || dbJobs[0].JID != jid {
		t.Fatalf("job was not written to db")
	}

	if _, ok := m.unpersistedJobs.Load(jid); ok {
		t.Fatalf("job should be removed from unpersistedJobs after successful DB sync")
	}

	m.Delete(jid)

	<-m.pending

	err = m.syncWithDB(ctx, action{typ: putJob, id: jid})
	if err != nil {
		t.Fatalf("syncWithDB putJob (delete) failed: %v", err)
	}

	dbJobs, _ = mockdb.ListJobs(ctx)
	if len(dbJobs) != 0 {
		t.Fatalf("job was not deleted from db")
	}

	if _, ok := m.deletedJobs.Load(jid); ok {
		t.Fatalf("job should be removed from deletedJobs after successful DB sync")
	}
}
