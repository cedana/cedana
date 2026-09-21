package utils

import (
	"context"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"testing"
	"time"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
)

// spawnWideTree forks `n` direct child processes under a parent shell and returns
// the parent PID and a cleanup. Each child is `sleep 60`, so they stay alive for
// the duration of the test. This mimics workloads (e.g. trtllm + torch inductor)
// that fan out to hundreds of descendants.
func spawnWideTree(t *testing.T, n int) (parentPID uint32, cleanup func()) {
	t.Helper()
	// One shell that backgrounds n sleeps and waits on them. Killing the shell
	// reaps all children.
	script := "for i in $(seq 1 " + strconv.Itoa(n) + "); do sleep 60 & done; wait"
	cmd := exec.Command("sh", "-c", script)
	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start parent shell: %v", err)
	}
	// Give the shell a moment to fork all children.
	time.Sleep(200 * time.Millisecond)
	return uint32(cmd.Process.Pid), func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}
}

func TestFillProcessState_MainPidNotExist(t *testing.T) {
	ctx := context.Background()
	state := &daemon.ProcessState{}

	nonExistentPID := uint32(999999)

	err := FillProcessState(ctx, nonExistentPID, state, true)

	if err == nil {
		t.Fatal("expected error when main PID does not exist, got nil")
	}

	if state.PID != nonExistentPID {
		t.Errorf("expected state.PID to be %d, got %d", nonExistentPID, state.PID)
	}
}

func TestFillProcessState_ChildExitsDuringWalk(t *testing.T) {
	ctx := context.Background()
	state := &daemon.ProcessState{}

	selfPID := uint32(os.Getpid())

	err := FillProcessState(ctx, selfPID, state, true)
	if err != nil {
		t.Fatalf("expected no error when children exit during walk, got: %v", err)
	}

	if state.PID != selfPID {
		t.Errorf("expected state.PID to be %d, got %d", selfPID, state.PID)
	}

	if state.Children == nil {
		t.Error("expected state.Children to be initialized, got nil")
	}
}

// Regression test for the trtllm-class hang: FillProcessState(deep=true) must
// honor ctx cancellation rather than walking every descendant. Originally the
// per-child loop ignored ctx.Err() and the walk re-globbed /proc per node.
func TestFillProcessState_DeepTreeRespectsDeadline(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping process-tree test in short mode")
	}

	const numChildren = 200
	parentPID, cleanup := spawnWideTree(t, numChildren)
	defer cleanup()

	const deadline = 200 * time.Millisecond
	const slack = 2 * time.Second // generous: we just want "not unbounded"

	ctx, cancel := context.WithTimeout(context.Background(), deadline)
	defer cancel()

	state := &daemon.ProcessState{}

	start := time.Now()
	_ = FillProcessState(ctx, parentPID, state, true)
	elapsed := time.Since(start)

	if elapsed > deadline+slack {
		t.Fatalf(
			"FillProcessState ignored context deadline: ran for %s with a %s deadline (over %d-child tree)",
			elapsed, deadline, numChildren,
		)
	}
}

// Regression test for the SendHeader-multiple-times daemon symptom: many
// concurrent callers all do a deep walk on the same wide tree. Each must honor
// its own ctx deadline.
func TestFillProcessState_DeepTreeConcurrentCallersRespectDeadline(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping process-tree test in short mode")
	}

	const numChildren = 150
	parentPID, cleanup := spawnWideTree(t, numChildren)
	defer cleanup()

	const deadline = 200 * time.Millisecond
	const slack = 2 * time.Second
	const callers = 8

	var wg sync.WaitGroup
	maxElapsed := int64(0)
	var mu sync.Mutex

	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), deadline)
			defer cancel()
			state := &daemon.ProcessState{}
			start := time.Now()
			_ = FillProcessState(ctx, parentPID, state, true)
			elapsed := time.Since(start)
			mu.Lock()
			if int64(elapsed) > maxElapsed {
				maxElapsed = int64(elapsed)
			}
			mu.Unlock()
		}()
	}
	wg.Wait()

	if time.Duration(maxElapsed) > deadline+slack {
		t.Fatalf(
			"slowest caller took %s with a %s deadline; FillProcessState is not honoring ctx",
			time.Duration(maxElapsed), deadline,
		)
	}
}

// Verifies the iterative walk descends through multiple levels (not just the
// direct children of the root). Mirrors trtllm: parent shell -> N MPI workers
// -> M inductor workers each.
func TestFillProcessState_DeepTreeMultiLevel(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping process-tree test in short mode")
	}

	const fanout = 4
	const grandchildrenPer = 5
	// Each fanout child spawns grandchildrenPer sleeps, then waits.
	script := "for i in $(seq 1 " + strconv.Itoa(fanout) + "); do " +
		"(for j in $(seq 1 " + strconv.Itoa(grandchildrenPer) + "); do sleep 60 & done; wait) & " +
		"done; wait"
	cmd := exec.Command("sh", "-c", script)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	time.Sleep(400 * time.Millisecond)
	parentPID := uint32(cmd.Process.Pid)

	state := &daemon.ProcessState{}
	if err := FillProcessState(context.Background(), parentPID, state, true); err != nil {
		t.Fatalf("FillProcessState: %v", err)
	}

	// Count all descendants.
	var count int
	var walk func(s *daemon.ProcessState)
	walk = func(s *daemon.ProcessState) {
		for _, c := range s.Children {
			count++
			walk(c)
		}
	}
	walk(state)

	// Expect at least fanout + fanout*grandchildrenPer = 24. Allow a margin
	// for the shell's own subshells / sleep processes.
	want := fanout + fanout*grandchildrenPer
	if count < want {
		t.Fatalf("expected at least %d descendants, got %d", want, count)
	}

	// Also verify multi-level structure: at least one direct child must itself
	// have children.
	hasGrandchild := false
	for _, c := range state.Children {
		if len(c.Children) > 0 {
			hasGrandchild = true
			break
		}
	}
	if !hasGrandchild {
		t.Fatalf("iterative walk did not descend past the first level")
	}
}

// BenchmarkFillProcessState_DeepTree measures wall time of the deep walk over a
// realistic-ish wide tree. The recursive impl is O(N · |/proc|) because each
// node calls gopsutil.Children which globs /proc; an iterative impl is O(N).
func BenchmarkFillProcessState_DeepTree(b *testing.B) {
	const numChildren = 200
	cmd := exec.Command("sh", "-c", "for i in $(seq 1 "+strconv.Itoa(numChildren)+"); do sleep 60 & done; wait")
	if err := cmd.Start(); err != nil {
		b.Fatalf("failed to start parent shell: %v", err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	time.Sleep(300 * time.Millisecond)
	parentPID := uint32(cmd.Process.Pid)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		state := &daemon.ProcessState{}
		_ = FillProcessState(context.Background(), parentPID, state, true)
	}
}

func TestFillProcessState_WithoutTree(t *testing.T) {
	ctx := context.Background()
	state := &daemon.ProcessState{}

	selfPID := uint32(os.Getpid())

	err := FillProcessState(ctx, selfPID, state, false)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if state.PID != selfPID {
		t.Errorf("expected state.PID to be %d, got %d", selfPID, state.PID)
	}

	if state.Children != nil {
		t.Error("expected state.Children to be nil when tree is false, got non-nil")
	}
}
