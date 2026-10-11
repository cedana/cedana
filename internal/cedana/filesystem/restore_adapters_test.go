package filesystem

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/config"
	cedana_io "github.com/cedana/cedana/pkg/io"
	"github.com/cedana/cedana/pkg/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// A checkpoint of three image files: as a directory, and as a tarball of it
func checkpointFiles(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range map[string]string{
		"pages-1.img":   string(bytes.Repeat([]byte("pages of the restored process "), 400)),
		"core-1.img":    "core image",
		"inventory.img": "inventory",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// tarball writes dir as a plain tar, so that a flipped byte in a file's content
// still untars and only the checksum tells
func tarball(t *testing.T, dir string) (path, sum string) {
	t.Helper()
	path = filepath.Join(t.TempDir(), "checkpoint.tar")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := cedana_io.Tar(dir, file, "tar", false); err != nil {
		t.Fatal(err)
	}
	file.Close()
	sum, err = checksumOfFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return path, sum
}

// flip changes one byte of the content of the pages image in a tar or a file
func flip(t *testing.T, path string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	i := bytes.Index(data, []byte("restored process"))
	if i < 0 {
		t.Fatal("the content to corrupt is not in the file")
	}
	data[i] ^= 0x20
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// restoreWith runs the adapter with a stand-in for the rest of the restore, which
// records whether it ran and what the images directory held
func restoreWith(t *testing.T, storage cedana_io.Storage, path, expected string) (resp *daemon.RestoreResp, ran bool, images []string, err error) {
	t.Helper()
	next := func(ctx context.Context, opts types.Opts, resp *daemon.RestoreResp, req *daemon.RestoreReq) (func() <-chan int, error) {
		ran = true
		entries, _ := os.ReadDir(req.GetCriu().GetImagesDir())
		for _, entry := range entries {
			images = append(images, entry.Name())
		}
		return nil, nil
	}
	resp = &daemon.RestoreResp{}
	req := &daemon.RestoreReq{Path: path, Checksum: expected}
	_, err = RestoreFilesystem(next)(context.Background(), types.Opts{Storage: storage}, resp, req)
	return resp, ran, images, err
}

func setVerify(t *testing.T, mode string) {
	t.Helper()
	previous := config.Global.Checkpoint.ChecksumVerify
	config.Global.Checkpoint.ChecksumVerify = mode
	t.Cleanup(func() { config.Global.Checkpoint.ChecksumVerify = previous })
}

func TestRestoreVerifiesALocalTarball(t *testing.T) {
	path, sum := tarball(t, checkpointFiles(t))
	for _, mode := range []string{config.CHECKSUM_VERIFY_WARN, config.CHECKSUM_VERIFY_STRICT} {
		t.Run(mode, func(t *testing.T) {
			setVerify(t, mode)
			resp, ran, images, err := restoreWith(t, &Storage{}, path, sum)
			if err != nil || !ran {
				t.Fatalf("restore with the recorded checksum: ran=%t err=%v", ran, err)
			}
			if resp.ChecksumResult != daemon.ChecksumResult_CHECKSUM_MATCH || resp.Checksum != sum {
				t.Fatalf("result = %v %s, want MATCH %s", resp.ChecksumResult, resp.Checksum, sum)
			}
			if len(images) != 3 {
				t.Fatalf("the images directory holds %v", images)
			}
		})
	}
}

func TestRestoreVerifiesCompressedTarballs(t *testing.T) {
	dir := checkpointFiles(t)
	setVerify(t, config.CHECKSUM_VERIFY_STRICT)
	for _, compression := range []string{"gzip", "lz4", "zlib"} {
		t.Run(compression, func(t *testing.T) {
			ext, err := cedana_io.ExtForCompression(compression)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "checkpoint"+ext)
			file, err := os.Create(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := cedana_io.Tar(dir, file, compression, false); err != nil {
				t.Fatal(err)
			}
			file.Close()
			sum, err := checksumOfFile(path)
			if err != nil {
				t.Fatal(err)
			}
			// The whole file is hashed, also the bytes after the end of the archive
			resp, ran, _, err := restoreWith(t, &Storage{}, path, sum)
			if err != nil || !ran || resp.ChecksumResult != daemon.ChecksumResult_CHECKSUM_MATCH {
				t.Fatalf("ran=%t err=%v result=%v checksum=%s want %s", ran, err, resp.ChecksumResult, resp.Checksum, sum)
			}
		})
	}
}

func TestRestoreOfACorruptedLocalTarball(t *testing.T) {
	path, sum := tarball(t, checkpointFiles(t))
	flip(t, path)

	t.Run("strict", func(t *testing.T) {
		setVerify(t, config.CHECKSUM_VERIFY_STRICT)
		resp, ran, _, err := restoreWith(t, &Storage{}, path, sum)
		if ran {
			t.Fatal("the restore must not run on a mismatch in strict mode")
		}
		if status.Code(err) != codes.FailedPrecondition {
			t.Fatalf("err = %v, want FailedPrecondition", err)
		}
		if resp.ChecksumResult != daemon.ChecksumResult_CHECKSUM_MISMATCH || resp.ChecksumReason != daemon.ChecksumReason_REASON_STORED {
			t.Fatalf("result = %v %v, want MISMATCH STORED: a local file is its own store", resp.ChecksumResult, resp.ChecksumReason)
		}
		if resp.Checksum == sum || resp.Checksum == "" {
			t.Fatalf("the response must carry the checksum read, got %q", resp.Checksum)
		}
		if !resp.ChecksumStrict {
			t.Fatal("the response must say the check was strict")
		}
	})
	t.Run("warn", func(t *testing.T) {
		setVerify(t, config.CHECKSUM_VERIFY_WARN)
		resp, ran, _, err := restoreWith(t, &Storage{}, path, sum)
		if err != nil || !ran {
			t.Fatalf("warn mode must restore: ran=%t err=%v", ran, err)
		}
		if resp.ChecksumResult != daemon.ChecksumResult_CHECKSUM_MISMATCH || resp.ChecksumReason != daemon.ChecksumReason_REASON_STORED {
			t.Fatalf("result = %v %v, want MISMATCH STORED", resp.ChecksumResult, resp.ChecksumReason)
		}
		if resp.ChecksumStrict {
			t.Fatal("warn mode must not say strict")
		}
	})
	t.Run("off", func(t *testing.T) {
		setVerify(t, config.CHECKSUM_VERIFY_OFF)
		resp, ran, _, err := restoreWith(t, &Storage{}, path, sum)
		if err != nil || !ran {
			t.Fatalf("off must restore: ran=%t err=%v", ran, err)
		}
		if resp.ChecksumResult != daemon.ChecksumResult_CHECKSUM_NOT_CHECKED || resp.Checksum != "" {
			t.Fatalf("off must compute nothing, got %v %q", resp.ChecksumResult, resp.Checksum)
		}
	})
	t.Run("no expected checksum", func(t *testing.T) {
		setVerify(t, config.CHECKSUM_VERIFY_STRICT)
		resp, ran, _, err := restoreWith(t, &Storage{}, path, "")
		if err != nil || !ran || resp.ChecksumResult != daemon.ChecksumResult_CHECKSUM_NOT_CHECKED {
			t.Fatalf("without an expected checksum: ran=%t err=%v result=%v", ran, err, resp.ChecksumResult)
		}
	})
	t.Run("another algorithm", func(t *testing.T) {
		setVerify(t, config.CHECKSUM_VERIFY_STRICT)
		resp, ran, _, err := restoreWith(t, &Storage{}, path, "sha256:00")
		if err != nil || !ran || resp.ChecksumResult != daemon.ChecksumResult_CHECKSUM_NOT_CHECKED {
			t.Fatalf("with a sha256 checksum: ran=%t err=%v result=%v", ran, err, resp.ChecksumResult)
		}
	})
}

func TestRestoreVerifiesALocalDirectory(t *testing.T) {
	dir := checkpointFiles(t)
	sum, err := checksumOfPath(dir)
	if err != nil {
		t.Fatal(err)
	}
	setVerify(t, config.CHECKSUM_VERIFY_STRICT)

	resp, ran, _, err := restoreWith(t, &Storage{}, dir, sum)
	if err != nil || !ran || resp.ChecksumResult != daemon.ChecksumResult_CHECKSUM_MATCH {
		t.Fatalf("directory with the recorded manifest: ran=%t err=%v result=%v", ran, err, resp.ChecksumResult)
	}

	// The files a restore rewrites are not part of the manifest
	if err := os.WriteFile(filepath.Join(dir, "criu-restore.log"), []byte("a previous restore"), 0o644); err != nil {
		t.Fatal(err)
	}
	if resp, _, _, err := restoreWith(t, &Storage{}, dir, sum); err != nil || resp.ChecksumResult != daemon.ChecksumResult_CHECKSUM_MATCH {
		t.Fatalf("a restore log must not change the manifest: err=%v result=%v", err, resp.ChecksumResult)
	}

	flip(t, filepath.Join(dir, "pages-1.img"))
	resp, ran, _, err = restoreWith(t, &Storage{}, dir, sum)
	if ran || status.Code(err) != codes.FailedPrecondition || resp.ChecksumReason != daemon.ChecksumReason_REASON_STORED {
		t.Fatalf("a changed file in strict mode: ran=%t err=%v reason=%v", ran, err, resp.ChecksumReason)
	}
}

// A remote store that holds the right bytes and reports value as its own
// checksum, while the first corruptReads downloads alter one byte in transfer
type flakyStore struct {
	Storage
	value        string
	corruptReads int32
	opens        atomic.Int32
}

func (s *flakyStore) IsRemote() bool { return true }

func (s *flakyStore) ChecksumPath(context.Context, string) (string, error) {
	if s.value == "" {
		return "", errors.New("no checksum held")
	}
	return s.value, nil
}

func (s *flakyStore) Open(ctx context.Context, path string) (io.ReadCloser, error) {
	n := s.opens.Add(1)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if n <= s.corruptReads {
		i := bytes.Index(data, []byte("restored process"))
		data[i] ^= 0x20
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func TestRestoreFromARemoteStore(t *testing.T) {
	path, sum := tarball(t, checkpointFiles(t))
	setVerify(t, config.CHECKSUM_VERIFY_STRICT)

	t.Run("transfer corrupted once, the second read restores", func(t *testing.T) {
		store := &flakyStore{value: sum, corruptReads: 1}
		resp, ran, _, err := restoreWith(t, store, path, sum)
		if err != nil || !ran || resp.ChecksumResult != daemon.ChecksumResult_CHECKSUM_MATCH {
			t.Fatalf("ran=%t err=%v result=%v", ran, err, resp.ChecksumResult)
		}
		if store.opens.Load() != 2 {
			t.Fatalf("opens = %d, want 2", store.opens.Load())
		}
	})
	t.Run("transfer corrupted twice", func(t *testing.T) {
		store := &flakyStore{value: sum, corruptReads: 2}
		resp, ran, _, err := restoreWith(t, store, path, sum)
		if ran || status.Code(err) != codes.FailedPrecondition || resp.ChecksumReason != daemon.ChecksumReason_REASON_TRANSFER {
			t.Fatalf("ran=%t err=%v reason=%v, want a failed restore with TRANSFER", ran, err, resp.ChecksumReason)
		}
	})
	t.Run("store holds no value", func(t *testing.T) {
		store := &flakyStore{corruptReads: 2}
		resp, ran, _, err := restoreWith(t, store, path, sum)
		if ran || resp.ChecksumReason != daemon.ChecksumReason_REASON_UNKNOWN || store.opens.Load() != 2 {
			t.Fatalf("ran=%t err=%v reason=%v opens=%d, want UNKNOWN after two reads", ran, err, resp.ChecksumReason, store.opens.Load())
		}
	})
	t.Run("store value differs, nothing downloaded", func(t *testing.T) {
		store := &flakyStore{value: "crc32c:00000000"}
		resp, ran, _, err := restoreWith(t, store, path, sum)
		if ran || status.Code(err) != codes.FailedPrecondition || resp.ChecksumReason != daemon.ChecksumReason_REASON_STORED {
			t.Fatalf("ran=%t err=%v reason=%v, want STORED", ran, err, resp.ChecksumReason)
		}
		if store.opens.Load() != 0 {
			t.Fatalf("opens = %d, the checkpoint must not be downloaded", store.opens.Load())
		}
	})
	t.Run("warn mode reads once and restores", func(t *testing.T) {
		setVerify(t, config.CHECKSUM_VERIFY_WARN)
		store := &flakyStore{value: sum, corruptReads: 1}
		resp, ran, _, err := restoreWith(t, store, path, sum)
		if err != nil || !ran || resp.ChecksumReason != daemon.ChecksumReason_REASON_TRANSFER || store.opens.Load() != 1 {
			t.Fatalf("ran=%t err=%v reason=%v opens=%d", ran, err, resp.ChecksumReason, store.opens.Load())
		}
	})
}
