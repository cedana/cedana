package gpu

import (
	"context"
	"errors"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"buf.build/gen/go/cedana/cedana-gpu/grpc/go/gpu/gpugrpc"
	"buf.build/gen/go/cedana/cedana-gpu/protocolbuffers/go/gpu"
	criu_proto "buf.build/gen/go/cedana/criu/protocolbuffers/go/criu"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
)

// A GPU controller client that answers Freeze and Unfreeze only
type fakeControllerClient struct {
	gpugrpc.ControllerClient
	freezeErr error
	freezes   atomic.Int32
	unfreezes atomic.Int32
}

func (f *fakeControllerClient) Freeze(ctx context.Context, in *gpu.FreezeReq, opts ...grpc.CallOption) (*gpu.FreezeResp, error) {
	f.freezes.Add(1)
	if f.freezeErr != nil {
		return nil, f.freezeErr
	}
	return &gpu.FreezeResp{}, nil
}

func (f *fakeControllerClient) Unfreeze(ctx context.Context, in *gpu.UnfreezeReq, opts ...grpc.CallOption) (*gpu.UnfreezeResp, error) {
	f.unfreezes.Add(1)
	return &gpu.UnfreezeResp{}, nil
}

func newFakePool(t *testing.T, client *fakeControllerClient) (*pool, *controller) {
	t.Helper()
	c := &controller{
		ID:               "test-controller",
		Terminated:       make(chan int, 1),
		ControllerClient: client,
	}
	p := &pool{}
	p.Store(c.ID, c)
	return p, c
}

func dumpOpts() *criu_proto.CriuOpts {
	return &criu_proto.CriuOpts{Pid: proto.Int32(1), LeaveRunning: proto.Bool(false)}
}

// waitGoroutines waits for the goroutine count to drop back to at most n
func waitGoroutines(t *testing.T, n int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for runtime.NumGoroutine() > n {
		if time.Now().After(deadline) {
			t.Fatalf("%d goroutines left, want at most %d", runtime.NumGoroutine(), n)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// A failed freeze must release the termination lock: CRIU calls no FinalizeDump after a
// failed InitializeDump, and a kept lock blocks the pool's sync and every later dump.
func TestInitializeDumpReleasesLockOnFailedFreeze(t *testing.T) {
	client := &fakeControllerClient{freezeErr: errors.New("Freeze failed for worker pid 89501")}
	p, c := newFakePool(t, client)
	callback := p.CRIUCallback(c.ID)

	before := runtime.NumGoroutine()
	err := callback.InitializeDumpFunc(context.Background(), dumpOpts())
	if err == nil {
		t.Fatal("InitializeDump succeeded with a failing freeze")
	}
	if !c.Termination.TryLock() {
		t.Fatal("the termination lock is still held after a failed freeze")
	}
	c.Termination.Unlock()

	// The freeze watcher ends with the call, though the controller never terminates
	waitGoroutines(t, before)
}

// A successful freeze keeps the lock for FinalizeDump. When the dump stops before the GPU
// dump, for example on a later callback, FinalizeDump unfreezes the process and unlocks.
func TestFinalizeDumpUnfreezesWhenGPUDumpNeverStarted(t *testing.T) {
	client := &fakeControllerClient{}
	p, c := newFakePool(t, client)
	callback := p.CRIUCallback(c.ID)

	before := runtime.NumGoroutine()
	if err := callback.InitializeDumpFunc(context.Background(), dumpOpts()); err != nil {
		t.Fatal(err)
	}
	if c.Termination.TryLock() {
		t.Fatal("the termination lock was released after a successful freeze")
	}
	if err := callback.FinalizeDumpFunc(context.Background(), dumpOpts(), errors.New("a later callback failed")); err != nil {
		t.Fatal(err)
	}
	if got := client.unfreezes.Load(); got != 1 {
		t.Fatalf("%d unfreezes, want 1", got)
	}
	if !c.Termination.TryLock() {
		t.Fatal("FinalizeDump did not release the termination lock")
	}
	c.Termination.Unlock()
	waitGoroutines(t, before)
}

// Without a successful freeze there is nothing to unfreeze
func TestFinalizeDumpDoesNotUnfreezeWithoutFreeze(t *testing.T) {
	client := &fakeControllerClient{freezeErr: errors.New("freeze failed")}
	p, c := newFakePool(t, client)
	callback := p.CRIUCallback(c.ID)

	if err := callback.InitializeDumpFunc(context.Background(), dumpOpts()); err == nil {
		t.Fatal("InitializeDump succeeded with a failing freeze")
	}
	// As NotifyCallbackMulti does for a callback that started before a later one failed
	c.Termination.Lock()
	if err := callback.FinalizeDumpFunc(context.Background(), dumpOpts(), errors.New("freeze failed")); err != nil {
		t.Fatal(err)
	}
	if got := client.unfreezes.Load(); got != 0 {
		t.Fatalf("%d unfreezes, want 0", got)
	}
}
