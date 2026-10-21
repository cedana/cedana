package gcs

import (
	"bytes"
	"context"
	"crypto/rand"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	cedana_config "github.com/cedana/cedana/pkg/config"
	cedana_io "github.com/cedana/cedana/pkg/io"
	"github.com/fsouza/fake-gcs-server/fakestorage"
)

// A GCS emulator in the test process, with one bucket, reached over HTTP as the
// daemon reaches one through emulator_host
func newEmulatedStorage(t *testing.T) *Storage {
	t.Helper()
	server, err := fakestorage.NewServerWithOptions(fakestorage.Options{
		Scheme:         "http",
		Host:           "127.0.0.1",
		InitialObjects: nil,
	})
	if err != nil {
		t.Fatalf("emulator: %v", err)
	}
	t.Cleanup(server.Stop)
	server.CreateBucketWithOpts(fakestorage.CreateBucketOpts{Name: "ckpt"})

	settings := cedana_config.Global.GCS
	t.Cleanup(func() { cedana_config.Global.GCS = settings })
	cedana_config.Global.GCS.EmulatorHost = server.URL()
	cedana_config.Global.Checkpoint.Checksum = true

	storage, err := NewStorage(context.Background())
	if err != nil {
		t.Fatalf("NewStorage: %v", err)
	}
	return storage.(*Storage)
}

func crc32cOf(b []byte) string {
	return cedana_io.FormatChecksum(crc32.Checksum(b, crc32.MakeTable(crc32.Castagnoli)))
}

func upload(t *testing.T, s *Storage, path string, payload []byte) *File {
	t.Helper()
	w, err := s.Create(context.Background(), path)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := w.Write(payload); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	return w.(*File)
}

// Larger than one chunk, so the upload is a resumable one of several requests
func payloadOf(t *testing.T, size int) []byte {
	t.Helper()
	b := make([]byte, size)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return b
}

func TestWriterChecksumIsTheBytesSent(t *testing.T) {
	s := newEmulatedStorage(t)
	payload := payloadOf(t, 2*CHUNK_SIZE+1234)
	file := upload(t, s, "gs://ckpt/checkpoint.tar.lz4", payload)

	want := crc32cOf(payload)
	if file.Checksum() != want {
		t.Fatalf("Checksum() = %s, want %s", file.Checksum(), want)
	}
	if file.stored != want {
		t.Fatalf("the store's value read back = %q, want %s", file.stored, want)
	}
	if cedana_io.ChecksumOfWriter(file) != want {
		t.Fatalf("the writer does not implement Checksummer")
	}

	got, err := s.ChecksumPath(context.Background(), "gs://ckpt/checkpoint.tar.lz4")
	if err != nil || got != want {
		t.Fatalf("ChecksumPath = %q, %v, want %s", got, err, want)
	}

	r, err := s.Open(context.Background(), "gs://ckpt/checkpoint.tar.lz4")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()
	read, err := io.ReadAll(r)
	if err != nil || !bytes.Equal(read, payload) {
		t.Fatalf("Open read %d bytes, %v; want the %d bytes written", len(read), err, len(payload))
	}
}

func TestWriterChecksumOffWhenTheSettingIsOff(t *testing.T) {
	s := newEmulatedStorage(t)
	cedana_config.Global.Checkpoint.Checksum = false
	t.Cleanup(func() { cedana_config.Global.Checkpoint.Checksum = true })

	file := upload(t, s, "gs://ckpt/off.tar", []byte("checkpoint"))
	if file.Checksum() != "" {
		t.Fatalf("Checksum() = %s, want none", file.Checksum())
	}
}

func TestCreateFailsWithoutTheBucket(t *testing.T) {
	s := newEmulatedStorage(t)
	if _, err := s.Create(context.Background(), "gs://missing/checkpoint.tar"); err == nil {
		t.Fatal("Create in a bucket that does not exist succeeded")
	}
}

func TestOpenMissingObject(t *testing.T) {
	s := newEmulatedStorage(t)
	_, err := s.Open(context.Background(), "gs://ckpt/missing.tar")
	if err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("Open = %v, want does not exist", err)
	}
	if _, err := s.ChecksumPath(context.Background(), "gs://ckpt/missing.tar"); err == nil {
		t.Fatal("ChecksumPath of a missing object succeeded")
	}
}

// A streamed checkpoint is a prefix of shard objects
func TestReadDirAndDeleteOfAStreamedCheckpoint(t *testing.T) {
	s := newEmulatedStorage(t)
	ctx := context.Background()
	for _, shard := range []string{"img-0.lz4", "img-1.lz4"} {
		upload(t, s, "gs://ckpt/dump/"+shard, []byte(shard))
	}
	upload(t, s, "gs://ckpt/dump-other/img-0.lz4", []byte("another checkpoint"))

	list, err := s.ReadDir(ctx, "gs://ckpt/dump")
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	slices.Sort(list)
	if !slices.Equal(list, []string{"dump/img-0.lz4", "dump/img-1.lz4"}) {
		t.Fatalf("ReadDir = %v", list)
	}

	if err := s.Delete(ctx, "gs://ckpt/dump"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if list, _ := s.ReadDir(ctx, "gs://ckpt/dump"); len(list) != 0 {
		t.Fatalf("shards left after Delete: %v", list)
	}
	if list, _ := s.ReadDir(ctx, "gs://ckpt/dump-other"); len(list) != 1 {
		t.Fatalf("Delete removed another checkpoint's objects: %v", list)
	}
}

func TestSanitizePath(t *testing.T) {
	s := &Storage{}
	for path, ok := range map[string]bool{
		"gs://b/k":         true,
		"gs://b/dir/k.tar": true,
		"gs://b":           false,
		"gs://b/":          false,
		"s3://b/k":         false,
		"gs://":            false,
	} {
		_, _, err := s.sanitizePath(path)
		if (err == nil) != ok {
			t.Errorf("sanitizePath(%q) error = %v, want ok=%t", path, err, ok)
		}
	}
}

func TestClientOptions(t *testing.T) {
	key := filepath.Join(t.TempDir(), "key.json")
	if err := os.WriteFile(key, []byte(`{"type":"service_account"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name     string
		settings cedana_config.GCS
		options  int
		fails    bool
	}{
		{"Ambient", cedana_config.GCS{CredentialsMode: "ambient"}, 0, false},
		{"ServiceAccountFile", cedana_config.GCS{CredentialsMode: "serviceAccount", ServiceAccountKey: key}, 1, false},
		{"ServiceAccountJSON", cedana_config.GCS{CredentialsMode: "serviceAccount", ServiceAccountKey: `{"type":"service_account"}`}, 1, false},
		{"ServiceAccountWithoutKey", cedana_config.GCS{CredentialsMode: "serviceAccount"}, 0, true},
		{"ServiceAccountMissingFile", cedana_config.GCS{CredentialsMode: "serviceAccount", ServiceAccountKey: key + ".missing"}, 0, true},
		{"UnknownMode", cedana_config.GCS{CredentialsMode: "static"}, 0, true},
		{"Emulator", cedana_config.GCS{CredentialsMode: "static", EmulatorHost: "localhost:4443"}, 3, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opts, err := ClientOptions(tc.settings)
			if (err != nil) != tc.fails || len(opts) != tc.options {
				t.Fatalf("ClientOptions = %d options, %v; want %d, fails=%t", len(opts), err, tc.options, tc.fails)
			}
		})
	}
	if got, _ := emulatorEndpoint("localhost:4443"); got != "http://localhost:4443/storage/v1/" {
		t.Fatalf("emulatorEndpoint = %s", got)
	}
	if got, _ := emulatorEndpoint("https://gcs:4443"); got != "https://gcs:4443/storage/v1/" {
		t.Fatalf("emulatorEndpoint = %s", got)
	}
}
