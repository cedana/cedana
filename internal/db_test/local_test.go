package db_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/internal/db"
)

// TestLocalDBPersistent tests the in-memory local db implementation.
// All tests here use a persistent db across multiple operations.
// Some tests are dependent on each other, so they should be run in order.
func TestLocalDBPersistent(t *testing.T) {
	ctx := context.TODO()
	path := filepath.Join(t.TempDir(), "cedana.db")
	t.Logf("using DB path: %s", path)
	db, err := db.NewSqliteDB(ctx, path)
	if err != nil {
		t.Fatalf("failed to create local db: %v", err)
	}

	n := 30
	testJobs := make([]*daemon.Job, n)
	testHosts := make([]*daemon.Host, n)
	for i := 0; i < n; i++ {
		testHosts[i] = generateRandomHost()
		testJobs[i] = generateRandomJob()
		testJobs[i].State.Host = testHosts[i]
	}

	t.Run("PutHost", func(t *testing.T) {
		err := db.PutHost(ctx, testHosts[0])
		if err != nil {
			t.Fatalf("failed to put host: %v", err)
		}
	})

	t.Run("PutHosts", func(t *testing.T) {
		for i := 0; i < n; i++ {
			err := db.PutHost(ctx, testHosts[i])
			if err != nil {
				t.Errorf("failed to put host: %v", err)
			}
		}
	})

	t.Run("ListHosts", func(t *testing.T) {
		hosts, err := db.ListHosts(ctx)
		if err != nil {
			t.Fatalf("failed to list hosts: %v", err)
		}
		if len(hosts) != n {
			t.Fatalf("expected %d hosts, got %d", n, len(hosts))
		}

		testSet := make(map[string]bool)
		foundSet := make(map[string]bool)
		for _, h := range testHosts {
			testSet[h.ID] = true
		}

		for _, h := range hosts {
			if _, ok := testSet[h.ID]; !ok {
				t.Errorf("unexpected host: %v", h)
			}
			foundSet[h.ID] = true
		}

		for _, h := range testHosts {
			if _, ok := foundSet[h.ID]; !ok {
				t.Errorf("missing host: %v", h)
			}
		}
	})

	t.Run("PutJob", func(t *testing.T) {
		err := db.PutJob(ctx, testJobs[0])
		if err != nil {
			t.Fatalf("failed to put job: %v", err)
		}
	})

	t.Run("PutJobs", func(t *testing.T) {
		for i := 1; i < n; i++ {
			err := db.PutJob(ctx, testJobs[i])
			if err != nil {
				t.Fatalf("failed to put job: %v", err)
			}
		}
	})

	t.Run("ListJobs", func(t *testing.T) {
		jobs, err := db.ListJobs(ctx)
		if err != nil {
			t.Fatalf("failed to list jobs: %v", err)
		}
		if len(jobs) != n {
			t.Fatalf("expected %d jobs, got %d", n, len(jobs))
		}

		testSet := make(map[string]bool)
		foundSet := make(map[string]bool)
		for _, j := range testJobs {
			testSet[j.JID] = true
		}

		for _, j := range jobs {
			if _, ok := testSet[j.JID]; !ok {
				t.Errorf("unexpected job: %v", j)
			}
			foundSet[j.JID] = true
		}

		for _, j := range testJobs {
			if _, ok := foundSet[j.JID]; !ok {
				t.Errorf("missing job: %v", j)
			}
		}
	})

	t.Run("DeleteHost", func(t *testing.T) {
		err := db.DeleteHost(ctx, testHosts[0].ID)
		if err != nil {
			t.Fatalf("failed to delete host: %v", err)
		}

		hosts, err := db.ListHosts(ctx)
		if err != nil {
			t.Fatalf("failed to list hosts: %v", err)
		}
		if len(hosts) != n-1 {
			t.Fatalf("expected %d hosts, got %d", n-1, len(hosts))
		}

		// this should also delete all jobs associated with the host
		jobs, err := db.ListJobs(ctx)
		if err != nil {
			t.Fatalf("failed to list jobs: %v", err)
		}
		if len(jobs) != n-1 {
			t.Fatalf("expected %d jobs, got %d", n-1, len(jobs))
		}
	})

	t.Run("DeleteJob", func(t *testing.T) {
		err := db.DeleteJob(ctx, testJobs[1].JID)
		if err != nil {
			t.Fatalf("failed to delete job: %v", err)
		}

		jobs, err := db.ListJobs(ctx)
		if err != nil {
			t.Fatalf("failed to list jobs: %v", err)
		}
		if len(jobs) != n-2 {
			t.Fatalf("expected %d jobs, got %d", n-2, len(jobs))
		}
	})

	os.Remove(path)
}

///////////////
/// Helpers ///
///////////////

func generateRandomJob() *daemon.Job {
	jid := fmt.Sprintf("%d", time.Now().UnixNano())

	return &daemon.Job{
		JID:   jid,
		Type:  "test",
		State: &daemon.ProcessState{PID: 1, IsRunning: true},
		Details: &daemon.Details{
			Process: &daemon.Process{
				PID:  1,
				Path: "random",
			},
		},
	}
}

func generateRandomHost() *daemon.Host {
	id := fmt.Sprintf("%d", time.Now().UnixNano())

	return &daemon.Host{
		ID:            id,
		Hostname:      "random",
		MAC:           "00:00:00:00:00:00",
		OS:            "linux",
		Platform:      "x86_64",
		KernelVersion: "5.4.0-42-generic",
		KernelArch:    "x86_64",
		CPU: &daemon.CPU{
			Count: 4,
		},
		Memory: &daemon.Memory{
			Total: 1024,
		},
	}
}
