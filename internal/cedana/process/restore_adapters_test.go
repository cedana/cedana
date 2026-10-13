package process

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/types"
)

// A serverless restore inherits the job's stdio from ours, but only the root task's: a
// descendant's descriptor 1 may be a redirection to a file of the job's own, which is
// to be restored from the images, not mapped to our stdout. The root task's stdout is
// on a mount the dump doesn't know (SLURM opens it before the job joins its namespace),
// so it's external and doesn't claim descriptor 1 first.
func TestInheritFilesForRestoreStdioOfRootOnly(t *testing.T) {
	outPath := filepath.Join(t.TempDir(), "job.out")
	if err := os.WriteFile(outPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	out := &daemon.File{Path: outPath, Fd: 1, MountID: 42, Inode: 1}
	marker := &daemon.File{Path: "/tmp/marker", Fd: 3, MountID: 9, Inode: 2}
	childStdout := &daemon.File{Path: "/tmp/marker", Fd: 1, MountID: 9, Inode: 2}
	state := &daemon.ProcessState{
		OpenFiles: []*daemon.File{{Path: "/dev/null", Fd: 0, MountID: 7, Inode: 3}, out, marker},
		Mounts:    []*daemon.Mount{{ID: 7}, {ID: 9}},
		Children:  []*daemon.ProcessState{{OpenFiles: []*daemon.File{childStdout}}},
	}

	var keys []string
	next := func(ctx context.Context, opts types.Opts, resp *daemon.RestoreResp, req *daemon.RestoreReq) (func() <-chan int, error) {
		for _, fd := range req.GetCriu().GetInheritFd() {
			keys = append(keys, fd.GetKey())
		}
		return nil, nil
	}

	opts := types.Opts{Serverless: true, InheritFdMap: map[string]int32{}}
	resp := &daemon.RestoreResp{State: state}
	if _, err := InheritFilesForRestore(next)(context.Background(), opts, resp, &daemon.RestoreReq{}); err != nil {
		t.Fatal(err)
	}

	if !slices.Contains(keys, "dev/null") {
		t.Errorf("root task's stdin not inherited, got %v", keys)
	}
	if slices.Contains(keys, "tmp/marker") {
		t.Errorf("descendant's redirected stdout inherited as ours, got %v", keys)
	}
}
