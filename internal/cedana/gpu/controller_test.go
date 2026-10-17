package gpu

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"buf.build/gen/go/cedana/cedana-gpu/grpc/go/gpu/gpugrpc"
	"buf.build/gen/go/cedana/cedana-gpu/protocolbuffers/go/gpu"
	criu_proto "buf.build/gen/go/cedana/criu/protocolbuffers/go/criu"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
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

// An Info that does not answer: it sends the deadline it was given, then returns only
// when its context ends
type silentInfoClient struct {
	gpugrpc.ControllerClient
	called chan infoDeadline
}

type infoDeadline struct {
	at  time.Time
	set bool
}

func (s *silentInfoClient) Info(ctx context.Context, in *gpu.InfoReq, opts ...grpc.CallOption) (*gpu.InfoResp, error) {
	at, set := ctx.Deadline()
	s.called <- infoDeadline{at, set}
	<-ctx.Done()
	return nil, ctx.Err()
}

// A controller that does not answer Info must not hold controller.Sync for ever: the pool
// sync calls it with contexts that have no deadline, and every dump waits on that sync.
func TestControllerSyncBoundsInfo(t *testing.T) {
	conn, err := grpc.NewClient("passthrough:///unused", grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	client := &silentInfoClient{called: make(chan infoDeadline, 1)}
	c := &controller{
		ID:               "test-controller",
		Address:          "unix:///nonexistent.sock",
		Terminated:       make(chan int, 1),
		ClientConn:       conn,
		ControllerClient: client,
	}

	// A caller with no deadline, as the pool's background loop; it cancels only so the
	// test does not wait the full INFO_TIMEOUT
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	start := time.Now()
	go func() { result <- c.Sync(ctx, false) }()

	var got infoDeadline
	select {
	case got = <-client.called:
	case <-time.After(2 * time.Second):
		t.Fatal("Sync never called Info")
	}
	cancel()
	if err := <-result; err == nil {
		t.Fatal("Sync succeeded with a controller that does not answer")
	}
	if !got.set {
		t.Fatal("Info got a context with no deadline")
	}
	if d := got.at.Sub(start); d > INFO_TIMEOUT+time.Second || d < INFO_TIMEOUT-time.Second {
		t.Fatalf("Info deadline %v after the call, want about %v", d, INFO_TIMEOUT)
	}
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

// Terminates c as Terminate does, under its termination lock, while a callback waits for it
func terminateWhileWaiting(t *testing.T, c *controller, callback func() error) error {
	t.Helper()
	c.Termination.Lock()
	result := make(chan error, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				result <- fmt.Errorf("panicked: %v", r)
			}
		}()
		result <- callback()
	}()
	time.Sleep(50 * time.Millisecond) // the callback is now waiting for the lock
	c.Terminating.Store(true)
	c.ClientConn = nil
	c.ControllerClient = nil
	c.Termination.Unlock()
	select {
	case err := <-result:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("callback did not return after the controller was terminated")
		return nil
	}
}

// A dump of a job whose GPU controller is being terminated (e.g. the job just exited) must
// fail, not use the terminated controller (nil client, which panicked and took down the daemon)
func TestInitializeDumpFailsOnTerminatedController(t *testing.T) {
	p, c := newFakePool(t, &fakeControllerClient{})
	callback := p.CRIUCallback(c.ID)

	err := terminateWhileWaiting(t, c, func() error {
		return callback.InitializeDumpFunc(context.Background(), dumpOpts())
	})
	if err == nil || strings.Contains(err.Error(), "panicked") {
		t.Fatalf("got %v, want an error for the terminated controller", err)
	}
	if !c.Termination.TryLock() {
		t.Fatal("the termination lock is still held")
	}
	c.Termination.Unlock()
}

func TestInitializeRestoreFailsOnTerminatedController(t *testing.T) {
	p, c := newFakePool(t, &fakeControllerClient{})
	callback := p.CRIUCallback(c.ID)

	err := terminateWhileWaiting(t, c, func() error {
		return callback.InitializeRestoreFunc(context.Background(), dumpOpts())
	})
	if err == nil || strings.Contains(err.Error(), "panicked") {
		t.Fatalf("got %v, want an error for the terminated controller", err)
	}
	if !c.Termination.TryLock() {
		t.Fatal("the termination lock is still held")
	}
	c.Termination.Unlock()
}
