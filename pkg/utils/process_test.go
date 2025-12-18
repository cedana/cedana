package utils

import (
	"context"
	"os"
	"testing"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
)

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
