package utils

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// A file written just before the timeout fires (e.g. by a process that writes
// its PID file and exits) must still be returned.
func TestWaitForFileExistsAtTimeout(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pid")
	if err := os.WriteFile(path, []byte("42"), 0o644); err != nil {
		t.Fatal(err)
	}

	timeout := make(chan int)
	close(timeout)

	data, err := WaitForFile(context.Background(), path, timeout)
	if err != nil {
		t.Fatalf("WaitForFile() error = %v", err)
	}
	if string(data) != "42" {
		t.Fatalf("WaitForFile() = %q, want %q", data, "42")
	}
}

func TestWaitForFileTimeout(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pid")

	timeout := make(chan int)
	close(timeout)

	if _, err := WaitForFile(context.Background(), path, timeout); err == nil {
		t.Fatal("WaitForFile() error = nil, want timeout")
	}
}
