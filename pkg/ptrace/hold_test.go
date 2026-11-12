package ptrace

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func start(t *testing.T, script string) int {
	t.Helper()
	cmd := exec.Command("sh", "-c", script)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() {
		cmd.Process.Kill()
		cmd.Wait()
	})
	return cmd.Process.Pid
}

func state(t *testing.T, pid int) string {
	t.Helper()
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		t.Fatalf("read stat: %v", err)
	}
	return strings.Fields(string(data[bytes.LastIndexByte(data, ')')+1:]))[0]
}

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func canTrace(t *testing.T) {
	t.Helper()
	pid := start(t, "sleep 30")
	h, err := Hold(func() ([]int, error) { return []int{pid}, nil }, func(int) bool { return false }, 5*time.Second)
	if err != nil || h.Len() == 0 {
		t.Skipf("cannot ptrace child processes here: %v", err)
	}
	h.Release()
}

func TestHoldStopsAndReleases(t *testing.T) {
	canTrace(t)
	pid := start(t, "while :; do :; done")

	h, err := Hold(func() ([]int, error) { return []int{pid}, nil }, func(int) bool { return false }, 5*time.Second)
	if err != nil {
		t.Fatalf("hold: %v", err)
	}
	if got := state(t, pid); got != "t" {
		t.Fatalf("held process state = %q, want t (tracing stop)", got)
	}
	if h.Len() != 1 {
		t.Fatalf("held %d threads, want 1", h.Len())
	}

	h.Release()
	eventually(t, "the process to run again", func() bool { return state(t, pid) == "R" })
	h.Release()
}

func TestHoldDeliversSignalsAfterRelease(t *testing.T) {
	canTrace(t)
	marker := filepath.Join(t.TempDir(), "got")
	pid := start(t, fmt.Sprintf(`trap 'echo got > %s' USR1; while :; do sleep 0.01; done`, marker))
	time.Sleep(100 * time.Millisecond)

	h, err := Hold(func() ([]int, error) { return []int{pid}, nil }, func(int) bool { return false }, 5*time.Second)
	if err != nil {
		t.Fatalf("hold: %v", err)
	}
	syscall.Kill(pid, syscall.SIGUSR1)
	time.Sleep(100 * time.Millisecond)
	if _, err := os.Stat(marker); err == nil {
		t.Fatalf("held process ran its signal handler")
	}

	h.Release()
	eventually(t, "the signal handler to run", func() bool {
		_, err := os.Stat(marker)
		return err == nil
	})
	if got := state(t, pid); got == "T" || got == "t" {
		t.Fatalf("released process state = %q, want running", got)
	}
}

func TestHoldSkips(t *testing.T) {
	canTrace(t)
	held := start(t, "while :; do :; done")
	skipped := start(t, "while :; do :; done")

	h, err := Hold(func() ([]int, error) { return []int{held, skipped}, nil }, func(pid int) bool { return pid == skipped }, 5*time.Second)
	if err != nil {
		t.Fatalf("hold: %v", err)
	}
	defer h.Release()

	if got := state(t, held); got != "t" {
		t.Fatalf("held process state = %q, want t", got)
	}
	if got := state(t, skipped); got == "t" || got == "T" {
		t.Fatalf("skipped process state = %q, want running", got)
	}
}

func TestHoldLeavesTracedThreads(t *testing.T) {
	canTrace(t)
	pid := start(t, "while :; do :; done")

	first, err := Hold(func() ([]int, error) { return []int{pid}, nil }, func(int) bool { return false }, 5*time.Second)
	if err != nil {
		t.Fatalf("first hold: %v", err)
	}
	defer first.Release()

	second, err := Hold(func() ([]int, error) { return []int{pid}, nil }, func(int) bool { return false }, 5*time.Second)
	if err != nil {
		t.Fatalf("second hold: %v", err)
	}
	defer second.Release()

	if second.Len() != 0 || second.Skipped() != 1 {
		t.Fatalf("second hold has %d threads and %d skipped, want 0 and 1", second.Len(), second.Skipped())
	}
}

func TestSession(t *testing.T) {
	sid, err := Session(os.Getpid())
	if err != nil {
		t.Fatalf("session: %v", err)
	}
	if want, _ := unix.Getsid(0); sid != want {
		t.Fatalf("session = %d, want %d", sid, want)
	}
}
