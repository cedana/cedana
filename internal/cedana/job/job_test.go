package job

import (
	"context"
	"os/exec"
	"strconv"
	"testing"
	"time"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/shirou/gopsutil/v4/process"
)

// spawnWideTree forks `n` direct child processes under a parent shell.
func spawnWideTree(t *testing.T, n int) (parentPID uint32, cleanup func()) {
	t.Helper()
	script := "for i in $(seq 1 " + strconv.Itoa(n) + "); do sleep 30 & done; wait"
	cmd := exec.Command("sh", "-c", script)
	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start parent shell: %v", err)
	}
	time.Sleep(200 * time.Millisecond)
	return uint32(cmd.Process.Pid), func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}
}

// Regression test: SyncDeep on a wide process tree must honor the request's
// context deadline. Previously latestState passed context.TODO() down to
// FillProcessState, dropping the deadline; that caused gRPC Get/Kill/Dump on
// trtllm-class workloads (~256 descendants) to hang.
func TestSyncDeepRespectsContextDeadline(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping process-tree test in short mode")
	}

	const numChildren = 200
	parentPID, cleanup := spawnWideTree(t, numChildren)
	defer cleanup()

	j := newJob("test", "process", &daemon.Host{ID: ""})
	j.proto.State.PID = parentPID
	j.proto.State.IsRunning = true
	// HostID empty -> latestState skips the "remote" branch.

	// Read cmdline via gopsutil (space-joined) so latestState's exact-match check passes.
	p, err := process.NewProcess(int32(parentPID))
	if err != nil {
		t.Fatalf("new process: %v", err)
	}
	cmdline, err := p.Cmdline()
	if err != nil {
		t.Fatalf("cmdline: %v", err)
	}
	j.proto.State.Cmdline = cmdline

	const deadline = 200 * time.Millisecond
	const slack = 2 * time.Second

	ctx, cancel := context.WithTimeout(context.Background(), deadline)
	defer cancel()

	done := make(chan struct{})
	start := time.Now()
	go func() {
		j.SyncDeep(ctx)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(deadline + slack):
		// fall through to the assertion below; SyncDeep is still running.
	}

	elapsed := time.Since(start)

	if elapsed > deadline+slack {
		t.Fatalf(
			"SyncDeep blocked for %s past a %s ctx deadline (over %d-child tree)",
			elapsed, deadline, numChildren,
		)
	}
}
