package upload

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestRegistry(t *testing.T) {
	ctx := context.Background()

	t.Run("WaitBeforeDuringAndAfter", func(t *testing.T) {
		r := NewRegistry()
		finish := r.Start("s3://bucket/dump.tar")

		results := make(chan Result, 3)
		wait := func() {
			result, err := r.Wait(ctx, "s3://bucket/dump.tar")
			if err != nil {
				t.Errorf("wait failed: %v", err)
			}
			results <- result
		}

		var wg sync.WaitGroup
		wg.Go(wait) // before the upload ends
		wg.Go(wait)

		time.Sleep(10 * time.Millisecond)
		finish(nil)
		wg.Wait()

		wait() // after the upload has ended

		for range 3 {
			if result := <-results; result.Err != nil {
				t.Fatalf("unexpected result %+v", result)
			}
		}
	})

	t.Run("Failure", func(t *testing.T) {
		r := NewRegistry()
		failure := errors.New("upload failed")
		r.Start("path")(failure)

		result, err := r.Wait(ctx, "path")
		if err != nil {
			t.Fatalf("wait failed: %v", err)
		}
		if !errors.Is(result.Err, failure) {
			t.Fatalf("expected the upload's error, got %v", result.Err)
		}
	})

	t.Run("NotFound", func(t *testing.T) {
		r := NewRegistry()
		if _, err := r.Wait(ctx, "unknown"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("expected ErrNotFound, got %v", err)
		}
	})

	t.Run("ContextDone", func(t *testing.T) {
		r := NewRegistry()
		r.Start("path")

		ctx, cancel := context.WithTimeout(ctx, 10*time.Millisecond)
		defer cancel()
		if _, err := r.Wait(ctx, "path"); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("expected the context's error, got %v", err)
		}
	})

	t.Run("Expires", func(t *testing.T) {
		r := NewRegistry()
		r.retention = 10 * time.Millisecond
		r.Start("finished")(nil)
		r.Start("running")

		time.Sleep(20 * time.Millisecond)

		if _, err := r.Wait(ctx, "finished"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("expected a finished upload to expire, got %v", err)
		}
		ctx, cancel := context.WithTimeout(ctx, 10*time.Millisecond)
		defer cancel()
		if _, err := r.Wait(ctx, "running"); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("expected a running upload to be kept, got %v", err)
		}
	})

	t.Run("Restarted", func(t *testing.T) {
		r := NewRegistry()
		r.Start("path")(errors.New("old"))
		r.Start("path")(nil)

		result, err := r.Wait(ctx, "path")
		if err != nil {
			t.Fatalf("wait failed: %v", err)
		}
		if result.Err != nil {
			t.Fatalf("expected the latest upload's result, got %v", result.Err)
		}
	})

	t.Run("FinishOnce", func(t *testing.T) {
		r := NewRegistry()
		finish := r.Start("path")
		finish(errors.New("first"))
		finish(nil)

		result, _ := r.Wait(ctx, "path")
		if result.Err == nil || result.Err.Error() != "first" {
			t.Fatalf("expected the first result to be kept, got %v", result.Err)
		}
	})

	t.Run("Nil", func(t *testing.T) {
		var r *Registry
		r.Start("path")(nil)
		if _, err := r.Wait(ctx, "path"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("expected ErrNotFound, got %v", err)
		}
	})
}
