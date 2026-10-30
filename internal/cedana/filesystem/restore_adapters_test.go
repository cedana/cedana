package filesystem

import (
	"bytes"
	"context"
	"fmt"
	criu_client "github.com/cedana/cedana/pkg/criu"
	"os"
	"path/filepath"
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

// damageHeader changes one byte of the first tar header in a file, so that the
// archive fails to extract at its first entry
func damageHeader(t *testing.T, path string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	data[0] ^= 0x20
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
	_, err = RestoreFilesystem(next)(context.Background(), types.Opts{Storage: storage, CRIUCallback: &criu_client.NotifyCallbackMulti{}}, resp, req)
	return resp, ran, images, err
}

func setVerify(t *testing.T, mode string) {
	t.Helper()
	previous := config.Global.Checkpoint.ChecksumVerify
	config.Global.Checkpoint.ChecksumVerify = mode
	t.Cleanup(func() { config.Global.Checkpoint.ChecksumVerify = previous })
}

// restoreAndReport runs the adapter with a stand-in for CRIU under the mem-verify
// plugin: it reports the manifest's checksum as read, when told one, then sends the
// notifications CRIU sends before the restored tasks resume
func restoreAndReport(t *testing.T, storage cedana_io.Storage, path, expected, reported string) (resp *daemon.RestoreResp, ran bool, images []string, err error) {
	t.Helper()
	next := func(ctx context.Context, opts types.Opts, resp *daemon.RestoreResp, req *daemon.RestoreReq) (func() <-chan int, error) {
		ran = true
		entries, _ := os.ReadDir(req.GetCriu().GetImagesDir())
		for _, entry := range entries {
			images = append(images, entry.Name())
		}
		if reported != "" {
			payload := fmt.Sprintf(`{"manifest":%q,"mode":"warn","files":%d,"verified":%d,"mismatched":0,"missing":0,"unverified":0,"gpu":0,"bytes":1,"failures":[]}`, reported, len(images), len(images))
			if err := opts.CRIUCallback.MemVerifyResult(ctx, []byte(payload)); err != nil {
				return nil, err
			}
		}
		if err := opts.CRIUCallback.PostRestore(ctx, 0); err != nil {
			return nil, err
		}
		return nil, opts.CRIUCallback.PreResume(ctx)
	}
	resp = &daemon.RestoreResp{}
	req := &daemon.RestoreReq{Path: path, Checksum: expected}
	_, err = RestoreFilesystem(next)(context.Background(), types.Opts{Storage: storage, CRIUCallback: &criu_client.NotifyCallbackMulti{}}, resp, req)
	return resp, ran, images, err
}

const recorded = "crc32c:0badf00d"

// A tarball is extracted and the plugin's report is compared with the recorded checksum
func TestRestoreVerifiesALocalTarball(t *testing.T) {
	path, _ := tarball(t, checkpointFiles(t))
	for _, mode := range []string{config.CHECKSUM_VERIFY_WARN, config.CHECKSUM_VERIFY_STRICT} {
		t.Run(mode, func(t *testing.T) {
			setVerify(t, mode)
			resp, ran, images, err := restoreAndReport(t, &Storage{}, path, recorded, recorded)
			if err != nil || !ran {
				t.Fatalf("restore with the recorded checksum: ran=%t err=%v", ran, err)
			}
			if resp.ChecksumResult != daemon.ChecksumResult_CHECKSUM_MATCH || resp.Checksum != recorded {
				t.Fatalf("result = %v %s, want MATCH %s", resp.ChecksumResult, resp.Checksum, recorded)
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
			ext, _ := cedana_io.ExtForCompression(compression)
			path := filepath.Join(t.TempDir(), "checkpoint.tar"+ext)
			file, err := os.Create(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := cedana_io.Tar(dir, file, compression, false); err != nil {
				t.Fatal(err)
			}
			file.Close()
			resp, ran, images, err := restoreAndReport(t, &Storage{}, path, recorded, recorded)
			if err != nil || !ran || resp.ChecksumResult != daemon.ChecksumResult_CHECKSUM_MATCH || len(images) != 3 {
				t.Fatalf("%s: ran=%t err=%v result=%v images=%v", compression, ran, err, resp.ChecksumResult, images)
			}
		})
	}
}

// A directory is used in place; nothing is read before CRIU, the plugin's report decides
func TestRestoreVerifiesALocalDirectory(t *testing.T) {
	dir := checkpointFiles(t)
	setVerify(t, config.CHECKSUM_VERIFY_STRICT)
	resp, ran, _, err := restoreAndReport(t, &Storage{}, dir, recorded, recorded)
	if err != nil || !ran || resp.ChecksumResult != daemon.ChecksumResult_CHECKSUM_MATCH {
		t.Fatalf("directory with the recorded checksum: ran=%t err=%v result=%v", ran, err, resp.ChecksumResult)
	}

	// The plugin read another manifest: the stored bytes differ
	setVerify(t, config.CHECKSUM_VERIFY_WARN)
	resp, ran, _, err = restoreAndReport(t, &Storage{}, dir, recorded, "crc32c:deadbeef")
	if err != nil || !ran || resp.ChecksumResult != daemon.ChecksumResult_CHECKSUM_MISMATCH || resp.ChecksumReason != daemon.ChecksumReason_REASON_STORED {
		t.Fatalf("warn, another manifest: ran=%t err=%v result=%v reason=%v", ran, err, resp.ChecksumResult, resp.ChecksumReason)
	}
	setVerify(t, config.CHECKSUM_VERIFY_STRICT)
	resp, _, _, err = restoreAndReport(t, &Storage{}, dir, recorded, "crc32c:deadbeef")
	if status.Code(err) != codes.FailedPrecondition || resp.ChecksumReason != daemon.ChecksumReason_REASON_STORED || !resp.ChecksumStrict {
		t.Fatalf("strict, another manifest: err=%v reason=%v strict=%t", err, resp.ChecksumReason, resp.ChecksumStrict)
	}
}

// Without a recorded checksum nothing is verified; without the plugin's report,
// warn mode restores unverified and strict mode does not
func TestRestoreWithoutAChecksumOrAReport(t *testing.T) {
	path, _ := tarball(t, checkpointFiles(t))
	setVerify(t, config.CHECKSUM_VERIFY_STRICT)
	resp, ran, _, err := restoreAndReport(t, &Storage{}, path, "", "")
	if err != nil || !ran || resp.ChecksumResult != daemon.ChecksumResult_CHECKSUM_NOT_CHECKED && resp.ChecksumResult != 0 {
		t.Fatalf("no recorded checksum: ran=%t err=%v result=%v", ran, err, resp.ChecksumResult)
	}
	setVerify(t, config.CHECKSUM_VERIFY_WARN)
	resp, ran, _, err = restoreAndReport(t, &Storage{}, path, recorded, "")
	if err != nil || !ran || resp.ChecksumResult != daemon.ChecksumResult_CHECKSUM_NOT_CHECKED {
		t.Fatalf("warn, no report: ran=%t err=%v result=%v", ran, err, resp.ChecksumResult)
	}
	setVerify(t, config.CHECKSUM_VERIFY_STRICT)
	resp, _, _, err = restoreAndReport(t, &Storage{}, path, recorded, "")
	if status.Code(err) != codes.FailedPrecondition || resp.ChecksumResult != daemon.ChecksumResult_CHECKSUM_NOT_CHECKED {
		t.Fatalf("strict, no report: err=%v result=%v", err, resp.ChecksumResult)
	}
}

// A damaged archive fails to extract, whatever the mode
func TestRestoreOfADamagedLocalTarball(t *testing.T) {
	path, _ := tarball(t, checkpointFiles(t))
	damageHeader(t, path)
	setVerify(t, config.CHECKSUM_VERIFY_WARN)
	_, ran, _, err := restoreWith(t, &Storage{}, path, recorded)
	if ran || status.Code(err) != codes.Internal {
		t.Fatalf("damaged tarball: ran=%t err=%v", ran, err)
	}
}
