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
		finish("sha256:abc", nil)
		wg.Wait()

		wait() // after the upload has ended

		for range 3 {
			if result := <-results; result.Checksum != "sha256:abc" || result.Err != nil {
				t.Fatalf("unexpected result %+v", result)
			}
		}
	})

	t.Run("Failure", func(t *testing.T) {
		r := NewRegistry()
		failure := errors.New("upload failed")
		r.Start("path")("sha256:abc", failure)

		result, err := r.Wait(ctx, "path")
		if err != nil {
			t.Fatalf("wait failed: %v", err)
		}
		if !errors.Is(result.Err, failure) {
			t.Fatalf("expected the upload's error, got %v", result.Err)
		}
		if result.Checksum != "" {
			t.Fatalf("expected no checksum for a failed upload, got %s", result.Checksum)
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
		r.Start("finished")("sha256:abc", nil)
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
		r.Start("path")("sha256:old", nil)
		r.Start("path")("sha256:new", nil)

		result, err := r.Wait(ctx, "path")
		if err != nil {
			t.Fatalf("wait failed: %v", err)
		}
		if result.Checksum != "sha256:new" {
			t.Fatalf("expected the latest upload's checksum, got %s", result.Checksum)
		}
	})

	t.Run("FinishOnce", func(t *testing.T) {
		r := NewRegistry()
		finish := r.Start("path")
		finish("sha256:first", nil)
		finish("sha256:second", nil)

		result, _ := r.Wait(ctx, "path")
		if result.Checksum != "sha256:first" {
			t.Fatalf("expected the first result to be kept, got %s", result.Checksum)
		}
	})

	t.Run("Nil", func(t *testing.T) {
		var r *Registry
		r.Start("path")("sha256:abc", nil)
		if _, err := r.Wait(ctx, "path"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("expected ErrNotFound, got %v", err)
		}
	})
}
