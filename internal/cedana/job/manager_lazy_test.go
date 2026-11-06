package job

import (
	"context"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"testing"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/internal/cedana/gpu"
)

// newTestManager returns a manager without a DB. Each returned code channel lets the test end the
// corresponding Manage routine; all are closed and waited on at cleanup.
func newTestManager(t *testing.T) (*ManagerLazy, func() chan int) {
	t.Helper()
	m := &ManagerLazy{
		pending: make(chan action, 64),
		wg:      &sync.WaitGroup{},
		gpus:    gpu.ManagerMissing{},
	}
	var codes []chan int
	t.Cleanup(func() {
		for _, c := range codes {
			close(c)
		}
		m.wg.Wait()
	})
	return m, func() chan int {
		c := make(chan int, 1)
		codes = append(codes, c)
		return c
	}
}

func startProcess(t *testing.T) uint32 {
	t.Helper()
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start process: %v", err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	return uint32(cmd.Process.Pid)
}

// Regression test: a job that only remembers a PID (loaded from a shared remote DB, possibly from
// another host, or whose process already exited) must not block managing a new process that has
// since been given the same PID.
func TestManageIgnoresStaleJobWithSamePID(t *testing.T) {
	pid := startProcess(t)
	m, newCode := newTestManager(t)

	stale := fromProto(&daemon.Job{
		JID:   "stale",
		Type:  "process",
		State: &daemon.ProcessState{PID: pid, Host: &daemon.Host{ID: "some-other-host"}},
	})
	m.jobs.Store(stale.JID, stale)

	for _, jid := range []string{"fresh", "dup"} {
		if _, err := m.New(jid, "process"); err != nil {
			t.Fatalf("New(%s): %v", jid, err)
		}
	}

	code := make(chan int, 1)
	if err := m.Manage(t.Context(), "fresh", pid, code); err != nil {
		t.Fatalf("Manage should ignore the stale job, got: %v", err)
	}

	// A second job for the same, actively managed PID must still be rejected, naming the owner.
	err := m.Manage(t.Context(), "dup", pid, newCode())
	if err == nil || !strings.Contains(err.Error(), "under job fresh") {
		t.Fatalf("expected conflict naming job fresh, got: %v", err)
	}

	// Once the managed process exits, its PID is free to be managed again.
	code <- 0
	<-m.Done("fresh")
	if err := m.Manage(t.Context(), "dup", pid, newCode()); err != nil {
		t.Fatalf("Manage after previous job exited: %v", err)
	}
}

// Concurrent Manage calls for the same PID under different jobs: exactly one may win.
func TestManageConcurrentSamePID(t *testing.T) {
	pid := startProcess(t)
	m, newCode := newTestManager(t)

	const n = 8
	codes := make([]chan int, n)
	for i := range n {
		if _, err := m.New(strconv.Itoa(i), "process"); err != nil {
			t.Fatalf("New: %v", err)
		}
		codes[i] = newCode()
	}

	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := range n {
		wg.Go(func() { errs[i] = m.Manage(context.WithoutCancel(t.Context()), strconv.Itoa(i), pid, codes[i]) })
	}
	wg.Wait()

	succeeded := 0
	for _, err := range errs {
		if err == nil {
			succeeded++
		}
	}
	if succeeded != 1 {
		t.Fatalf("expected exactly one Manage to succeed, got %d: %v", succeeded, errs)
	}
}
