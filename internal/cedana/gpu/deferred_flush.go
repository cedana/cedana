package gpu

import (
	"context"
	"errors"
	"sync"
)

type DeferredFlushes struct {
	mu    sync.Mutex
	waits []func(context.Context) error
}

type deferredFlushesKey struct{}

func WithDeferredFlushes(ctx context.Context) (context.Context, *DeferredFlushes) {
	d := &DeferredFlushes{}
	return context.WithValue(ctx, deferredFlushesKey{}, d), d
}

func deferredFlushesFrom(ctx context.Context) *DeferredFlushes {
	d, _ := ctx.Value(deferredFlushesKey{}).(*DeferredFlushes)
	return d
}

func (d *DeferredFlushes) add(wait func(context.Context) error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.waits = append(d.waits, wait)
}

func (d *DeferredFlushes) Pending() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.waits) > 0
}

func (d *DeferredFlushes) Wait(ctx context.Context) error {
	d.mu.Lock()
	waits := d.waits
	d.waits = nil
	d.mu.Unlock()

	errs := make([]error, len(waits))
	var wg sync.WaitGroup
	for i, wait := range waits {
		wg.Go(func() { errs[i] = wait(ctx) })
	}
	wg.Wait()
	return errors.Join(errs...)
}
