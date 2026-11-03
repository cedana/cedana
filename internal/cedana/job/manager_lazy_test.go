package job

import (
	"context"
	"os/exec"
	"strings"
	"sync"
	"testing"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/internal/cedana/gpu"
)

func newTestManager() *ManagerLazy {
	return &ManagerLazy{
		pending: make(chan action, 64),
		wg:      &sync.WaitGroup{},
		gpus:    gpu.ManagerMissing{},
	}
}

// Regression test: a job that only remembers a PID (loaded from a shared remote DB, possibly from
// another host, or whose process already exited) must not block managing a new process that has
// since been given the same PID.
func TestManageIgnoresStaleJobWithSamePID(t *testing.T) {
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start process: %v", err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	pid := uint32(cmd.Process.Pid)

	m := newTestManager()
	stale := fromProto(&daemon.Job{
		JID:   "stale",
		Type:  "process",
		State: &daemon.ProcessState{PID: pid, Host: &daemon.Host{ID: "some-other-host"}},
	})
	m.jobs.Store(stale.JID, stale)

	if _, err := m.New("fresh", "process"); err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	code := make(chan int, 1)
	if err := m.Manage(ctx, "fresh", pid, code); err != nil {
		t.Fatalf("Manage should ignore the stale job, got: %v", err)
	}

	// A second job for the same, actively managed PID must still be rejected, naming the owner.
	if _, err := m.New("dup", "process"); err != nil {
		t.Fatalf("New: %v", err)
	}
	err := m.Manage(ctx, "dup", pid, make(chan int, 1))
	if err == nil || !strings.Contains(err.Error(), "under job fresh") {
		t.Fatalf("expected conflict naming job fresh, got: %v", err)
	}

	// Once the managed process exits, its PID is free to be managed again.
	code <- 0
	<-m.Done("fresh")
	if err := m.Manage(ctx, "dup", pid, make(chan int, 1)); err != nil {
		t.Fatalf("Manage after previous job exited: %v", err)
	}
}
