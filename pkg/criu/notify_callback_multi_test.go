package criu

import (
	"context"
	"errors"
	"strings"
	"testing"

	"buf.build/gen/go/cedana/criu/protocolbuffers/go/criu"
)

// A callback that records which of its hooks ran, and with what error
type recordingCallback struct {
	name      string
	calls     *[]string
	initErr   error
	finalErr  error
	finalSeen []error
}

func (r *recordingCallback) notify() *NotifyCallback {
	return &NotifyCallback{
		Name: r.name,
		InitializeDumpFunc: func(ctx context.Context, opts *criu.CriuOpts) error {
			*r.calls = append(*r.calls, r.name+".InitializeDump")
			return r.initErr
		},
		InitializeRestoreFunc: func(ctx context.Context, opts *criu.CriuOpts) error {
			*r.calls = append(*r.calls, r.name+".InitializeRestore")
			return r.initErr
		},
		FinalizeDumpFunc: func(ctx context.Context, opts *criu.CriuOpts, err error) error {
			*r.calls = append(*r.calls, r.name+".FinalizeDump")
			r.finalSeen = append(r.finalSeen, err)
			return r.finalErr
		},
		FinalizeRestoreFunc: func(ctx context.Context, opts *criu.CriuOpts, err error) error {
			*r.calls = append(*r.calls, r.name+".FinalizeRestore")
			r.finalSeen = append(r.finalSeen, err)
			return r.finalErr
		},
	}
}

// Three callbacks a, b, c, included in that order, so they run c, b, a
func newRecordingMulti() (*NotifyCallbackMulti, map[string]*recordingCallback, *[]string) {
	calls := &[]string{}
	byName := map[string]*recordingCallback{}
	multi := &NotifyCallbackMulti{}
	for _, name := range []string{"a", "b", "c"} {
		r := &recordingCallback{name: name, calls: calls}
		byName[name] = r
		multi.Include(r.notify())
	}
	return multi, byName, calls
}

func assertCalls(t *testing.T, got *[]string, want ...string) {
	t.Helper()
	if strings.Join(*got, " ") != strings.Join(want, " ") {
		t.Fatalf("calls %v, want %v", *got, want)
	}
}

// b fails after c started: c is finalized with b's error, b is not, a never starts
func TestInitializeDumpFinalizesStartedCallbacks(t *testing.T) {
	multi, cb, calls := newRecordingMulti()
	cb["b"].initErr = errors.New("b cannot start")

	err := multi.InitializeDump(context.Background(), &criu.CriuOpts{})
	if err == nil || !strings.Contains(err.Error(), "b cannot start") {
		t.Fatalf("got %v, want b's error", err)
	}
	assertCalls(t, calls, "c.InitializeDump", "b.InitializeDump", "c.FinalizeDump")
	if len(cb["c"].finalSeen) != 1 || cb["c"].finalSeen[0] == nil || !strings.Contains(cb["c"].finalSeen[0].Error(), "b cannot start") {
		t.Fatalf("c was finalized with %v, want b's error", cb["c"].finalSeen)
	}
}

func TestInitializeRestoreFinalizesStartedCallbacks(t *testing.T) {
	multi, cb, calls := newRecordingMulti()
	cb["a"].initErr = errors.New("a cannot start")

	err := multi.InitializeRestore(context.Background(), &criu.CriuOpts{})
	if err == nil || !strings.Contains(err.Error(), "a cannot start") {
		t.Fatalf("got %v, want a's error", err)
	}
	assertCalls(t, calls,
		"c.InitializeRestore", "b.InitializeRestore", "a.InitializeRestore",
		"c.FinalizeRestore", "b.FinalizeRestore")
}

// The first callback to start failing finalizes nothing
func TestInitializeDumpFirstFailureFinalizesNothing(t *testing.T) {
	multi, cb, calls := newRecordingMulti()
	cb["c"].initErr = errors.New("c cannot start")

	if err := multi.InitializeDump(context.Background(), &criu.CriuOpts{}); err == nil {
		t.Fatal("InitializeDump succeeded")
	}
	assertCalls(t, calls, "c.InitializeDump")
}

// c fails to finalize: b and a are still finalized, with the dump error, not c's
func TestFinalizeDumpRunsEveryCallback(t *testing.T) {
	multi, cb, calls := newRecordingMulti()
	cb["c"].finalErr = errors.New("c cannot finalize")
	dumpErr := errors.New("the dump failed")

	err := multi.FinalizeDump(context.Background(), &criu.CriuOpts{}, dumpErr)
	if err == nil || !strings.Contains(err.Error(), "c cannot finalize") {
		t.Fatalf("got %v, want c's error", err)
	}
	assertCalls(t, calls, "c.FinalizeDump", "b.FinalizeDump", "a.FinalizeDump")
	for _, name := range []string{"a", "b", "c"} {
		if seen := cb[name].finalSeen; len(seen) != 1 || !errors.Is(seen[0], dumpErr) {
			t.Fatalf("%s was finalized with %v, want the dump error", name, seen)
		}
	}
}

func TestFinalizeRestoreRunsEveryCallback(t *testing.T) {
	multi, cb, calls := newRecordingMulti()
	cb["b"].finalErr = errors.New("b cannot finalize")
	cb["a"].finalErr = errors.New("a cannot finalize")

	err := multi.FinalizeRestore(context.Background(), &criu.CriuOpts{}, nil)
	if err == nil || !strings.Contains(err.Error(), "b cannot finalize") || !strings.Contains(err.Error(), "a cannot finalize") {
		t.Fatalf("got %v, want both errors joined", err)
	}
	assertCalls(t, calls, "c.FinalizeRestore", "b.FinalizeRestore", "a.FinalizeRestore")
}
