package gpu

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cedana/cedana/pkg/config"
	"github.com/cedana/cedana/pkg/plugins"
	"github.com/gofrs/flock"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

type syncer interface {
	Sync(ctx context.Context) error
}

func syncReturns(t *testing.T, s syncer, ctx context.Context) error {
	t.Helper()
	result := make(chan error, 1)
	go func() { result <- s.Sync(ctx) }()
	select {
	case err := <-result:
		return err
	case <-time.After(2 * time.Second):
		t.Fatal("Sync is still waiting for the running sync")
		return nil
	}
}

// A caller that finds a sync running stops waiting when its context ends. A running sync
// can wait on a controller's termination for a long time, and a dump must not wait with it.
func TestSyncWaiterStopsOnContext(t *testing.T) {
	simple := &ManagerSimple{}
	for name, s := range map[string]syncer{
		"ManagerSimple": simple,
		"ManagerPool":   &ManagerPool{ManagerSimple: &ManagerSimple{}},
	} {
		t.Run(name, func(t *testing.T) {
			var m *ManagerSimple
			switch v := s.(type) {
			case *ManagerSimple:
				m = v
			case *ManagerPool:
				m = v.ManagerSimple
			}

			done, err := m.waitOrStartSync(context.Background()) // the running sync
			if err != nil || done == nil {
				t.Fatalf("could not start a sync: %v", err)
			}
			defer done()

			ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
			defer cancel()
			if err := syncReturns(t, s, ctx); !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("got %v, want the context's error", err)
			}
		})
	}
}

// A caller that finds a sync running returns when that sync ends
func TestSyncWaiterReturnsWhenSyncEnds(t *testing.T) {
	m := &ManagerSimple{}
	done, err := m.waitOrStartSync(context.Background())
	if err != nil || done == nil {
		t.Fatalf("could not start a sync: %v", err)
	}

	result := make(chan error, 1)
	go func() { result <- m.Sync(context.Background()) }()

	select {
	case err := <-result:
		t.Fatalf("Sync returned %v before the running sync ended", err)
	case <-time.After(50 * time.Millisecond):
	}
	done()
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Sync did not return after the running sync ended")
	}
}

// At most one caller runs the sync at a time, and every caller returns
func TestSyncRunsOneAtATime(t *testing.T) {
	m := &ManagerSimple{}
	var running, most atomic.Int32
	var wg sync.WaitGroup
	for range 32 {
		wg.Go(func() {
			done, err := m.waitOrStartSync(context.Background())
			if err != nil {
				t.Error(err)
				return
			}
			if done == nil {
				return // waited for another caller's sync
			}
			n := running.Add(1)
			for {
				prev := most.Load()
				if n <= prev || most.CompareAndSwap(prev, n) {
					break
				}
			}
			time.Sleep(time.Millisecond)
			running.Add(-1)
			done()
		})
	}
	wg.Wait()
	if most.Load() != 1 {
		t.Fatalf("%d syncs ran at once, want 1", most.Load())
	}
}

// A plugins manager whose GPU plugin has a binary that does not exist, so a spawn fails
type fakePlugins struct {
	plugins.Manager
	gets atomic.Int32
}

func (f *fakePlugins) Get(name string) *plugins.Plugin {
	f.gets.Add(1)
	return &plugins.Plugin{Binaries: []plugins.Binary{{Name: "nonexistent", InstallDir: "/nonexistent"}}}
}

// A pool manager with no controller sockets on disk, so only the controllers a test stores
func newTestPoolManager(t *testing.T, poolSize int) (*ManagerPool, *fakePlugins) {
	t.Helper()
	gpuConfig := config.Global.GPU
	t.Cleanup(func() { config.Global.GPU = gpuConfig })
	config.Global.GPU.SockDir = t.TempDir()
	config.Global.GPU.LogDir = ""
	config.Global.GPU.Debug = false

	fake := &fakePlugins{}
	return &ManagerPool{ManagerSimple: &ManagerSimple{plugins: fake}, poolSize: poolSize}, fake
}

// Captures the global log for the test
func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	logger := log.Logger
	t.Cleanup(func() { log.Logger = logger })
	buf := &bytes.Buffer{}
	log.Logger = zerolog.New(buf).Level(zerolog.DebugLevel)
	return buf
}

// A stale controller that a dump or a restore holds must not make the sync wait: the sync
// skips it, leaves it as it was, refills the pool, and terminates it once the lock is free
func TestPoolSyncSkipsHeldController(t *testing.T) {
	m, fake := newTestPoolManager(t, 1)
	c := newStaleController(t, &m.controllers)

	c.Termination.Lock() // the dump
	if err := syncReturns(t, m, context.Background()); err != nil {
		t.Fatal(err)
	}

	if m.controllers.Get(c.ID) == nil {
		t.Fatal("the held controller is no longer in the pool")
	}
	if _, ok := m.held[c.ID]; !ok {
		t.Fatal("the held controller is not recorded as skipped")
	}
	booking := flock.New(c.Booking.Path())
	if locked, _ := booking.TryLock(); !locked {
		t.Fatal("the sync kept the booking lock of the held controller")
	}
	booking.Unlock()
	if fake.gets.Load() == 0 {
		t.Fatal("the sync did not go on to refill the pool")
	}

	c.Termination.Unlock() // FinalizeDump
	if err := syncReturns(t, m, context.Background()); err != nil {
		t.Fatal(err)
	}
	if m.controllers.Get(c.ID) != nil {
		t.Fatal("the sync did not terminate the controller once the lock was free")
	}
	if len(m.held) != 0 {
		t.Fatalf("%d controllers still recorded as skipped", len(m.held))
	}
}

// A controller held for more than HELD_WARN_AFTER gets one warning, not one per sync
func TestPoolSyncWarnsOnceForHeldController(t *testing.T) {
	m, _ := newTestPoolManager(t, 0)
	c := newStaleController(t, &m.controllers)
	logs := captureLog(t)

	c.Termination.Lock()
	defer c.Termination.Unlock()

	const warning = "held by a dump or restore for more than 1 min"
	for i := range 7 {
		if err := syncReturns(t, m, context.Background()); err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			if strings.Contains(logs.String(), warning) {
				t.Fatal("warned on the first skip")
			}
			m.held[c.ID].since = time.Now().Add(-2 * HELD_WARN_AFTER)
		}
	}
	if n := strings.Count(logs.String(), warning); n != 1 {
		t.Fatalf("%d warnings for 7 syncs, want 1", n)
	}
	if n := strings.Count(logs.String(), "skipping stale GPU controller held by a dump or restore"); n != 7 {
		t.Fatalf("%d skips logged for 7 syncs, want 7", n)
	}
}

// The sync logs the stderr of a stale controller only when it captured some: a controller
// found on disk has no stderr buffer, and must not log "<nil>"
func TestPoolSyncLogsCapturedStderr(t *testing.T) {
	tests := []struct {
		name   string
		errBuf *bytes.Buffer
		want   int
	}{
		{"found on disk", nil, 0},
		{"spawned, no stderr", &bytes.Buffer{}, 0},
		{"spawned, with stderr", bytes.NewBufferString("cuda init failed"), 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, _ := newTestPoolManager(t, 0)
			c := newStaleController(t, &m.controllers)
			c.ErrBuf = tt.errBuf
			logs := captureLog(t)

			if err := syncReturns(t, m, context.Background()); err != nil {
				t.Fatal(err)
			}
			if m.controllers.Get(c.ID) != nil {
				t.Fatal("the sync did not terminate the stale controller")
			}
			if n := strings.Count(logs.String(), "stale GPU controller captured stderr"); n != tt.want {
				t.Fatalf("%d stderr logs, want %d", n, tt.want)
			}
			if strings.Contains(logs.String(), "<nil>") {
				t.Fatal("logged <nil>")
			}
			if tt.want > 0 && !strings.Contains(logs.String(), "cuda init failed") {
				t.Fatal("the stderr log does not carry the captured stderr")
			}
		})
	}
}
