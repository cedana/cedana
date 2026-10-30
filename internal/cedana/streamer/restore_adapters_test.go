package streamer

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"testing"

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
func restoreStreamed(t *testing.T, storage cedana_io.Storage, path, expected, reported string, streams int32, pid int) (*daemon.RestoreResp, *restoreRun, error) {
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
		if reported != "" {
			payload := fmt.Sprintf(`{"manifest":%q,"mode":"warn","files":%d,"verified":%d,"mismatched":0,"missing":0,"unverified":0,"gpu":0,"bytes":1,"failures":[]}`, reported, run.read, run.read)
			if err := opts.CRIUCallback.MemVerifyResult(ctx, []byte(payload)); err != nil {
				return nil, err
			}
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

// The mem-verify plugin reports the manifest's checksum as it read the images through
// the streamer; the callback compares it with the recorded one at pre-resume
func TestStreamedRestoreVerifiesThePluginsReport(t *testing.T) {
	skipWithoutStreamer(t)
	const streams = 2
	path, _ := streamedCheckpoint(t, streams)
	const recorded = "crc32c:0badf00d"

	setVerifyMode(t, config.CHECKSUM_VERIFY_STRICT)
	resp, run, err := restoreStreamed(t, &filesystem.Storage{}, path, recorded, recorded, streams, 0)
	if err != nil || !run.ran || run.read != 6 || run.atPreResume != daemon.ChecksumResult_CHECKSUM_MATCH {
		t.Fatalf("strict, the recorded checksum reported: err=%v run=%+v resp=%v", err, run, resp)
	}

	setVerifyMode(t, config.CHECKSUM_VERIFY_WARN)
	resp, run, err = restoreStreamed(t, &filesystem.Storage{}, path, recorded, "crc32c:deadbeef", streams, 0)
	if err != nil || !run.ran || resp.ChecksumResult != daemon.ChecksumResult_CHECKSUM_MISMATCH || resp.ChecksumReason != daemon.ChecksumReason_REASON_STORED {
		t.Fatalf("warn, another manifest reported: err=%v run=%+v resp=%v", err, run, resp)
	}

	setVerifyMode(t, config.CHECKSUM_VERIFY_STRICT)
	_, run, err = restoreStreamed(t, &filesystem.Storage{}, path, recorded, "crc32c:deadbeef", streams, 0)
	if status.Code(err) != codes.FailedPrecondition || status.Code(run.preResumeErr) != codes.FailedPrecondition {
		t.Fatalf("strict, another manifest reported: err=%v preResumeErr=%v", err, run.preResumeErr)
	}

	// Without a recorded checksum nothing is verified
	_, run, err = restoreStreamed(t, &filesystem.Storage{}, path, "", "", streams, 0)
	if err != nil || !run.ran || run.atPreResume != 0 {
		t.Fatalf("no recorded checksum: err=%v run=%+v", err, run)
	}
}
