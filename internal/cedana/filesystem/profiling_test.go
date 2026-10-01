package filesystem

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/keys"
	"github.com/cedana/cedana/pkg/profiling"
	"github.com/cedana/cedana/pkg/types"
	"github.com/spf13/afero"
)

func TestLocalCheckpointImageBytes(t *testing.T) {
	data := &profiling.Data{}
	ctx := context.WithValue(context.Background(), keys.PROFILING_CONTEXT_KEY, data)
	resp := &daemon.DumpResp{}
	opts := types.Opts{Storage: &Storage{}}
	next := func(ctx context.Context, opts types.Opts, resp *daemon.DumpResp, req *daemon.DumpReq) (func() <-chan int, error) {
		return nil, afero.WriteFile(opts.DumpFs, "test.img", []byte("checkpoint"), 0o600)
	}
	_, err := DumpFilesystem(next)(ctx, opts, resp, &daemon.DumpReq{Dir: t.TempDir(), Name: "dump", Compression: "none"})
	if err != nil {
		t.Fatal(err)
	}
	if data.IO != 10 {
		t.Fatalf("dump image bytes = %d, want 10", data.IO)
	}

	data.IO = 0
	restore := func(ctx context.Context, opts types.Opts, resp *daemon.RestoreResp, req *daemon.RestoreReq) (func() <-chan int, error) {
		_, err := os.Stat(filepath.Join(req.Criu.GetImagesDir(), "test.img"))
		return nil, err
	}
	_, err = RestoreFilesystem(restore)(ctx, opts, &daemon.RestoreResp{}, &daemon.RestoreReq{Path: resp.Paths[0]})
	if err != nil {
		t.Fatal(err)
	}
	if data.IO != 10 {
		t.Fatalf("restore image bytes = %d, want 10", data.IO)
	}
}
