package utils

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// A file written right before the timeout fires, e.g. a pid file written by a runtime that
// then exits (crun --detach), is still returned rather than timing out
func TestWaitForFileWrittenBeforeTimeout(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pid")
	os.WriteFile(path, []byte("1234"), 0o644)

	timeout := make(chan struct{})
	close(timeout) // already fired, before the first tick

	data, err := WaitForFile(context.Background(), path, timeout)
	if err != nil {
		t.Fatalf("got error %v, want the file", err)
	}
	if string(data) != "1234" {
		t.Errorf("got %q, want %q", data, "1234")
	}
}

func TestWaitForFileTimeout(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing")

	timeout := make(chan struct{})
	close(timeout)

	if _, err := WaitForFile(context.Background(), path, timeout); err == nil {
		t.Error("got no error for a file that never appeared")
	}
}
