package gpu

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
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
