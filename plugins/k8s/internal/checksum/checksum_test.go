package checksum

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	cedana_io "github.com/cedana/cedana/pkg/io"
	"google.golang.org/grpc"
)

// A daemon whose paths are maps of entry name to content; a path with one entry
// named "" is a tarball
type fakeDaemon struct {
	paths   map[string]map[string]string
	dirs    map[string][]string
	readErr error
}

func (f *fakeDaemon) ListPath(_ context.Context, req *daemon.ListPathReq, _ ...grpc.CallOption) (*daemon.ListPathResp, error) {
	files, ok := f.paths[req.GetPath()]
	if !ok {
		return nil, errors.New("not found")
	}
	resp := &daemon.ListPathResp{}
	for name := range files {
		resp.Entries = append(resp.Entries, &daemon.PathEntry{Name: name})
	}
	for _, dir := range f.dirs[req.GetPath()] {
		resp.Entries = append(resp.Entries, &daemon.PathEntry{Name: dir, IsDir: true})
	}
	return resp, nil
}

func (f *fakeDaemon) ReadPath(_ context.Context, req *daemon.ReadPathReq, _ ...grpc.CallOption) (io.ReadCloser, error) {
	if f.readErr != nil {
		return nil, f.readErr
	}
	content, ok := f.paths[req.GetPath()][req.GetEntry()]
	if !ok {
		return nil, errors.New("no such entry")
	}
	return io.NopCloser(strings.NewReader(content)), nil
}

func sumOf(s string) string {
	sum, _ := cedana_io.ChecksumOf(bytes.NewReader([]byte(s)))
	return sum
}

func TestPathOfATarball(t *testing.T) {
	d := &fakeDaemon{paths: map[string]map[string]string{"/ckpt/dump.tar.lz4": {"": "compressed tarball bytes"}}}
	got, err := Path(context.Background(), d, "/ckpt/dump.tar.lz4")
	if err != nil {
		t.Fatal(err)
	}
	if want := sumOf("compressed tarball bytes"); got != want {
		t.Fatalf("Path = %s, want %s", got, want)
	}
}

func TestPathOfADirectoryIsItsManifest(t *testing.T) {
	d := &fakeDaemon{
		paths: map[string]map[string]string{"/ckpt/dump": {
			"pages-1.img":   "pages",
			"core-1.img":    "core",
			"criu-dump.log": "ignored",
			"stats-dump":    "ignored",
		}},
		dirs: map[string][]string{"/ckpt/dump": {"sub"}},
	}
	got, err := Path(context.Background(), d, "/ckpt/dump")
	if err != nil {
		t.Fatal(err)
	}
	// In order of name, without the excluded files and the sub-directory
	want := cedana_io.ManifestChecksum([]cedana_io.ManifestEntry{
		{Name: "core-1.img", Checksum: sumOf("core")},
		{Name: "pages-1.img", Checksum: sumOf("pages")},
	})
	if got != want {
		t.Fatalf("Path = %s, want %s", got, want)
	}
}

func TestPathOfAnEmptyDirectoryHasNoChecksum(t *testing.T) {
	d := &fakeDaemon{paths: map[string]map[string]string{"/ckpt/dump": {"criu-dump.log": "x"}}}
	got, err := Path(context.Background(), d, "/ckpt/dump")
	if err != nil || got != "" {
		t.Fatalf("Path = %q, %v", got, err)
	}
}

func TestPathReportsAReadError(t *testing.T) {
	d := &fakeDaemon{paths: map[string]map[string]string{"/ckpt/dump.tar.lz4": {"": "x"}}, readErr: errors.New("gone")}
	if _, err := Path(context.Background(), d, "/ckpt/dump.tar.lz4"); err == nil {
		t.Fatal("expected an error")
	}
}
