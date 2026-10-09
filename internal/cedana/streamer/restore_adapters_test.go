package streamer

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"testing"
	"time"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/internal/cedana/filesystem"
	"github.com/cedana/cedana/pkg/config"
	criu_client "github.com/cedana/cedana/pkg/criu"
	cedana_io "github.com/cedana/cedana/pkg/io"
	"github.com/cedana/cedana/pkg/types"
	"github.com/cedana/cedana/pkg/upload"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const testStreamerBinary = "/usr/local/bin/cedana-image-streamer"

// streamedCheckpoint takes a local streamed dump of a few images and returns its
// directory and the manifest of its shards as stored
func streamedCheckpoint(t *testing.T, streams int32) (string, string) {
	t.Helper()
	writeImages := func(ctx context.Context, opts types.Opts, resp *daemon.DumpResp, req *daemon.DumpReq) (func() <-chan int, error) {
		for i := range 6 {
			file, err := opts.DumpFs.Create(fmt.Sprintf("pages-%d.img", i))
			if err != nil {
				return nil, err
			}
			if _, err := file.Write([]byte(fmt.Sprintf("pages of image %d, %0512d", i, i))); err != nil {
				file.Close()
				return nil, err
			}
			if err := file.Close(); err != nil {
				return nil, err
			}
		}
		return nil, nil
	}
	opts := types.Opts{
		WG:           &sync.WaitGroup{},
		CRIUCallback: &criu_client.NotifyCallbackMulti{},
		Storage:      &filesystem.Storage{},
		Plugins:      &streamingPlugins{streamerBinary: testStreamerBinary},
		Uploads:      upload.NewRegistry(),
	}
	req := &daemon.DumpReq{Dir: t.TempDir(), Name: "dump", Compression: "lz4", Streams: streams}
	resp := &daemon.DumpResp{}
	if _, err := DumpFilesystem(streams)(writeImages)(context.Background(), opts, resp, req); err != nil {
		t.Fatalf("dump failed: %v", err)
	}
	opts.WG.Wait()
	path := resp.Paths[0]
	return path, manifestOf(t, path, streams, ".lz4")
}

// A stand-in for the restored process, to see whether a strict mismatch kills it
func standIn(t *testing.T) *exec.Cmd {
	t.Helper()
	cmd := exec.Command("sleep", "60")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
	return cmd
}

type restoreRun struct {
	ran          bool
	read         int
	atPreResume  daemon.ChecksumResult // the result as it stood when CRIU's hooks before resume had returned
	preResumeErr error
}

// restoreStreamed runs the streamer restore adapter with a stand-in for CRIU: it
// reads every image through the streaming fs, then sends the notifications CRIU
// sends before the restored tasks resume
func restoreStreamed(t *testing.T, storage cedana_io.Storage, path, expected string, streams int32, pid int) (*daemon.RestoreResp, *restoreRun, error) {
	t.Helper()
	run := &restoreRun{}
	next := func(ctx context.Context, opts types.Opts, resp *daemon.RestoreResp, req *daemon.RestoreReq) (func() <-chan int, error) {
		run.ran = true
		fs := opts.DumpFs.(*Fs)
		names, err := fs.glob("*")
		if err != nil {
			return nil, err
		}
		for _, name := range names {
			file, err := fs.Open(name)
			if err != nil {
				return nil, err
			}
			if _, err := io.ReadAll(file); err != nil {
				file.Close()
				return nil, err
			}
			file.Close()
			run.read++
		}
		run.preResumeErr = opts.CRIUCallback.PostRestore(ctx, int32(pid))
		if run.preResumeErr == nil {
			run.preResumeErr = opts.CRIUCallback.PreResume(ctx)
		}
		run.atPreResume = resp.ChecksumResult
		return nil, run.preResumeErr
	}
	opts := types.Opts{
		WG:           &sync.WaitGroup{},
		CRIUCallback: &criu_client.NotifyCallbackMulti{},
		Storage:      storage,
		Plugins:      &streamingPlugins{streamerBinary: testStreamerBinary},
	}
	resp := &daemon.RestoreResp{}
	req := &daemon.RestoreReq{Path: path, Checksum: expected}
	_, err := RestoreFilesystem(streams)(next)(context.Background(), opts, resp, req)
	return resp, run, err
}

func setVerifyMode(t *testing.T, mode string) {
	t.Helper()
	previous := config.Global.Checkpoint.ChecksumVerify
	config.Global.Checkpoint.ChecksumVerify = mode
	t.Cleanup(func() { config.Global.Checkpoint.ChecksumVerify = previous })
}

func skipWithoutStreamer(t *testing.T) {
	t.Helper()
	if _, err := os.Stat(testStreamerBinary); os.IsNotExist(err) {
		t.Skipf("streamer binary not found at %s, skipping integration test", testStreamerBinary)
	}
}

func TestStreamedRestoreVerifiesBeforeTheProcessResumes(t *testing.T) {
	skipWithoutStreamer(t)
	config.Global.Checkpoint.Checksum = true
	SHARDS_READ_TIMEOUT = 10 * time.Second
	streams := int32(2)
	path, sum := streamedCheckpoint(t, streams)

	t.Run("match", func(t *testing.T) {
		setVerifyMode(t, config.CHECKSUM_VERIFY_STRICT)
		process := standIn(t)
		resp, run, err := restoreStreamed(t, &filesystem.Storage{}, path, sum, streams, process.Process.Pid)
		if err != nil || run.read != 6 {
			t.Fatalf("restore: err=%v images read=%d", err, run.read)
		}
		if run.atPreResume != daemon.ChecksumResult_CHECKSUM_MATCH || resp.Checksum != sum {
			t.Fatalf("at pre-resume the result was %v, checksum %s; want MATCH %s, every shard read by then", run.atPreResume, resp.Checksum, sum)
		}
		if process.Process.Signal(syscall.Signal(0)) != nil {
			t.Fatal("a match must leave the restored process alone")
		}
	})
	t.Run("strict mismatch kills the process before it resumes", func(t *testing.T) {
		setVerifyMode(t, config.CHECKSUM_VERIFY_STRICT)
		process := standIn(t)
		resp, run, err := restoreStreamed(t, &filesystem.Storage{}, path, "crc32c:00000000", streams, process.Process.Pid)
		if status.Code(err) != codes.FailedPrecondition || run.preResumeErr == nil {
			t.Fatalf("err = %v, hook err = %v; want the restore to fail with FailedPrecondition from CRIU's hook", err, run.preResumeErr)
		}
		if resp.ChecksumResult != daemon.ChecksumResult_CHECKSUM_MISMATCH || resp.ChecksumReason != daemon.ChecksumReason_REASON_STORED {
			t.Fatalf("result = %v %v, want MISMATCH STORED", resp.ChecksumResult, resp.ChecksumReason)
		}
		done := make(chan error, 1)
		go func() { done <- process.Wait() }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("the restored process must be killed on a strict mismatch")
		}
	})
	t.Run("warn mismatch resumes", func(t *testing.T) {
		setVerifyMode(t, config.CHECKSUM_VERIFY_WARN)
		process := standIn(t)
		resp, run, err := restoreStreamed(t, &filesystem.Storage{}, path, "crc32c:00000000", streams, process.Process.Pid)
		if err != nil || run.preResumeErr != nil {
			t.Fatalf("warn mode must restore: err=%v pre-resume=%v", err, run.preResumeErr)
		}
		if resp.ChecksumResult != daemon.ChecksumResult_CHECKSUM_MISMATCH {
			t.Fatalf("result = %v, want MISMATCH recorded", resp.ChecksumResult)
		}
		if process.Process.Signal(syscall.Signal(0)) != nil {
			t.Fatal("warn mode must leave the restored process alone")
		}
	})
}

// A remote store whose own value of every shard differs from what was recorded
type badShardStore struct {
	remoteStorage
}

func (s *badShardStore) ChecksumPath(context.Context, string) (string, error) {
	return "crc32c:00000000", nil
}

func TestStreamedRestoreFailsFastOnAStoreValueThatDiffers(t *testing.T) {
	skipWithoutStreamer(t)
	config.Global.Checkpoint.Checksum = true
	streams := int32(2)
	path, sum := streamedCheckpoint(t, streams)
	setVerifyMode(t, config.CHECKSUM_VERIFY_STRICT)

	resp, run, err := restoreStreamed(t, &badShardStore{}, path, sum, streams, 0)
	if run.ran {
		t.Fatal("the restore must not start when the store's value differs")
	}
	if status.Code(err) != codes.FailedPrecondition || resp.ChecksumReason != daemon.ChecksumReason_REASON_STORED {
		t.Fatalf("err=%v reason=%v, want FailedPrecondition with STORED", err, resp.ChecksumReason)
	}
}
