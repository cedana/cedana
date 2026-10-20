package job

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	criu_proto "buf.build/gen/go/cedana/criu/protocolbuffers/go/criu"
	"github.com/cedana/cedana/pkg/types"
	"github.com/cedana/cedana/pkg/upload"
	"google.golang.org/protobuf/proto"
)

// checkpointRecorder is a Manager with one running job, that keeps the checkpoints added to it
type checkpointRecorder struct {
	Manager
	job *Job

	mu    sync.Mutex
	added map[string]time.Time
}

func (r *checkpointRecorder) Get(ctx context.Context, jid string) *Job {
	return r.job
}

func (r *checkpointRecorder) AddCheckpoint(jid string, paths []string, at time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, path := range paths {
		r.added[path] = at
	}
}

func (r *checkpointRecorder) paths() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var paths []string
	for path := range r.added {
		paths = append(paths, path)
	}
	slices.Sort(paths)
	return paths
}

// A managed dump saves a path whose upload still runs only once the upload has succeeded,
// with the time of the dump, and never saves one whose upload failed
func TestManageDumpSavesPendingPathsOnlyOnceStored(t *testing.T) {
	jobs := &checkpointRecorder{
		job: fromProto(&daemon.Job{
			JID:     "job",
			Type:    "process",
			State:   &daemon.ProcessState{IsRunning: true},
			Details: &daemon.Details{JID: proto.String("job")},
		}),
		added: make(map[string]time.Time),
	}
	uploads := upload.NewRegistry()
	var wg sync.WaitGroup

	var finishStored, finishFailed func(string, error)
	dump := func(ctx context.Context, opts types.Opts, resp *daemon.DumpResp, req *daemon.DumpReq) (func() <-chan int, error) {
		finishStored = opts.Uploads.Start("/dumps/stored.tar")
		finishFailed = opts.Uploads.Start("/dumps/failed.tar")
		resp.Paths = []string{"/dumps/dir", "/dumps/stored.tar", "/dumps/failed.tar"}
		resp.Pending = []string{"/dumps/stored.tar", "/dumps/failed.tar"}
		return nil, nil
	}

	before := time.Now()
	req := &daemon.DumpReq{Details: &daemon.Details{JID: proto.String("job")}, Criu: &criu_proto.CriuOpts{LeaveRunning: proto.Bool(true)}}
	_, err := ManageDump(jobs)(dump)(context.Background(), types.Opts{Uploads: uploads, WG: &wg}, &daemon.DumpResp{}, req)
	if err != nil {
		t.Fatalf("ManageDump: %v", err)
	}

	if got, want := jobs.paths(), []string{"/dumps/dir"}; !slices.Equal(got, want) {
		t.Fatalf("saved before the uploads ended = %v, want %v", got, want)
	}

	time.Sleep(10 * time.Millisecond)
	finishStored("crc32c:00000000", nil)
	finishFailed("", errors.New("upload failed"))
	wg.Wait()

	if got, want := jobs.paths(), []string{"/dumps/dir", "/dumps/stored.tar"}; !slices.Equal(got, want) {
		t.Fatalf("saved after the uploads ended = %v, want %v", got, want)
	}
	if at := jobs.added["/dumps/stored.tar"]; at.Before(before) || !at.Equal(jobs.added["/dumps/dir"]) {
		t.Fatalf("the stored path was saved at %v, want the time of the dump %v", at, jobs.added["/dumps/dir"])
	}
}
