package upload

// Keeps track of uploads that continue in the background after a dump has returned,
// so that a caller can wait for one to end and learn how it went.

import (
	"context"
	"errors"
	"sync"
	"time"
)

// How long the result of an upload is kept after it has ended
const RETENTION = 1 * time.Hour

var ErrNotFound = errors.New("no upload found for path")

type Result struct {
	Checksum string // of the path as stored, "crc32c:<hex>"; empty if the upload failed or nothing was computed
	Err      error  // why the upload failed; nil if it succeeded
}

type upload struct {
	done     chan struct{}
	result   Result
	finished time.Time
}

type Registry struct {
	mu        sync.Mutex
	uploads   map[string]*upload
	retention time.Duration
}

func NewRegistry() *Registry {
	return &Registry{
		uploads:   make(map[string]*upload),
		retention: RETENTION,
	}
}

// Start records that an upload to path is in progress. The returned function
// must be called once, when the upload has ended, with the checksum of the path
// as stored, if one was computed, and the error, if it failed.
// An upload to the same path that was started earlier is replaced.
func (r *Registry) Start(path string) (finish func(checksum string, err error)) {
	if r == nil {
		return func(string, error) {}
	}

	u := &upload{done: make(chan struct{})}

	r.mu.Lock()
	r.expire()
	r.uploads[path] = u
	r.mu.Unlock()

	var once sync.Once
	return func(checksum string, err error) {
		once.Do(func() {
			if err != nil {
				checksum = ""
			}
			r.mu.Lock()
			u.result = Result{Checksum: checksum, Err: err}
			u.finished = time.Now()
			r.mu.Unlock()
			close(u.done)
		})
	}
}

// Record keeps the result of a path whose checksum is already known when the dump
// returns, such as a streamed checkpoint written locally, so that a caller learns it
// the same way as the outcome of an upload: Wait returns at once.
func (r *Registry) Record(path string, checksum string) {
	if r == nil {
		return
	}
	r.Start(path)(checksum, nil)
}

// Wait blocks until the upload to path has ended, and returns its result.
// Returns ErrNotFound if no upload to path is known, which is also the case
// once its result has been kept for longer than the retention period.
func (r *Registry) Wait(ctx context.Context, path string) (Result, error) {
	if r == nil {
		return Result{}, ErrNotFound
	}

	r.mu.Lock()
	r.expire()
	u, ok := r.uploads[path]
	r.mu.Unlock()
	if !ok {
		return Result{}, ErrNotFound
	}

	select {
	case <-u.done:
	case <-ctx.Done():
		return Result{}, ctx.Err()
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	return u.result, nil
}

// Forgets uploads that ended longer ago than the retention period.
// Must be called with the lock held.
func (r *Registry) expire() {
	for path, u := range r.uploads {
		if !u.finished.IsZero() && time.Since(u.finished) > r.retention {
			delete(r.uploads, path)
		}
	}
}
