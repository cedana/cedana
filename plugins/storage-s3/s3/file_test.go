package s3

import (
	"context"
	"encoding/base64"
	"hash/crc32"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	cedana_config "github.com/cedana/cedana/pkg/config"
	cedana_io "github.com/cedana/cedana/pkg/io"
)

// A store that accepts one object and answers the upload with the CRC32C it
// chooses to report: that of the bytes it received, another value, or none.
type fakeStore struct {
	report func(received []byte) (checksum string, checksumType string)
	body   []byte
}

func (f *fakeStore) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, "unexpected "+r.Method, http.StatusBadRequest)
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// The SDK sends the body aws-chunked with a trailing checksum; the object
	// is the chunk payload. One chunk suffices for the sizes here
	f.body = unchunk(body)
	if f.report != nil {
		if sum, typ := f.report(f.body); sum != "" {
			w.Header().Set("x-amz-checksum-crc32c", sum)
			if typ != "" {
				w.Header().Set("x-amz-checksum-type", typ)
			}
		}
	}
	w.Header().Set("ETag", `"etag"`)
	w.WriteHeader(http.StatusOK)
}

// unchunk returns the payload of an aws-chunked body, or the body as is
func unchunk(body []byte) []byte {
	// "<hex size>[;chunk-signature=...]\r\n<payload>\r\n0\r\n<trailers>"
	var size int
	i := 0
	for ; i < len(body); i++ {
		c := body[i]
		switch {
		case c >= '0' && c <= '9':
			size = size*16 + int(c-'0')
		case c >= 'a' && c <= 'f':
			size = size*16 + int(c-'a'+10)
		case c >= 'A' && c <= 'F':
			size = size*16 + int(c-'A'+10)
		default:
			goto end
		}
	}
end:
	for i < len(body) && body[i] != '\n' {
		i++
	}
	i++
	if size == 0 || i+size > len(body) {
		return body
	}
	return body[i : i+size]
}

func crc32cOf(b []byte) string {
	return cedana_io.FormatChecksum(crc32.Checksum(b, crc32.MakeTable(crc32.Castagnoli)))
}

func encodeCRC32C(b []byte) string {
	sum := crc32.Checksum(b, crc32.MakeTable(crc32.Castagnoli))
	return base64.StdEncoding.EncodeToString([]byte{byte(sum >> 24), byte(sum >> 16), byte(sum >> 8), byte(sum)})
}

func uploadThrough(t *testing.T, store *fakeStore, payload []byte) *File {
	t.Helper()
	server := httptest.NewServer(store)
	t.Cleanup(server.Close)

	client := s3.New(s3.Options{
		Region:       "us-east-1",
		Credentials:  credentials.NewStaticCredentialsProvider("key", "secret", ""),
		BaseEndpoint: aws.String(server.URL),
		UsePathStyle: true,
	})
	cedana_config.Global.Checkpoint.Checksum = true

	file := NewFile(context.Background(), client, "bucket", "checkpoint.tar.lz4")
	if _, err := file.Write(payload); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if string(store.body) != string(payload) {
		t.Fatalf("the store received %q, want %q", store.body, payload)
	}
	return file
}

func TestWriterChecksumIsTheBytesSent(t *testing.T) {
	payload := []byte("the checkpoint as sent")
	store := &fakeStore{report: func(received []byte) (string, string) {
		return encodeCRC32C(received), "FULL_OBJECT"
	}}
	file := uploadThrough(t, store, payload)
	if got, want := file.Checksum(), crc32cOf(payload); got != want {
		t.Fatalf("checksum = %s, want %s", got, want)
	}
	if file.stored != crc32cOf(payload) {
		t.Fatalf("the store's value was not read back: %q", file.stored)
	}
}

func TestWriterChecksumWhenTheStoreGivesNone(t *testing.T) {
	payload := []byte("the checkpoint as sent")
	file := uploadThrough(t, &fakeStore{}, payload)
	if got, want := file.Checksum(), crc32cOf(payload); got != want {
		t.Fatalf("checksum = %s, want the writer's own %s", got, want)
	}
}

func TestWriterChecksumWhenTheStoreGivesAComposite(t *testing.T) {
	payload := []byte("the checkpoint as sent")
	store := &fakeStore{report: func([]byte) (string, string) {
		return encodeCRC32C([]byte("parts")), "COMPOSITE"
	}}
	file := uploadThrough(t, store, payload)
	if got, want := file.Checksum(), crc32cOf(payload); got != want {
		t.Fatalf("checksum = %s, want the writer's own %s", got, want)
	}
	if file.stored != "" {
		t.Fatalf("a composite value must not be kept: %q", file.stored)
	}
}

// Open question 26: a store value that differs from the writer's is logged,
// the upload stands, and the writer's value is the checksum
func TestWriterChecksumWhenTheStoreDiffers(t *testing.T) {
	payload := []byte("the checkpoint as sent")
	store := &fakeStore{report: func([]byte) (string, string) {
		return encodeCRC32C([]byte("other bytes")), "FULL_OBJECT"
	}}
	file := uploadThrough(t, store, payload)
	if got, want := file.Checksum(), crc32cOf(payload); got != want {
		t.Fatalf("checksum = %s, want the writer's own %s", got, want)
	}
	if got, want := file.stored, crc32cOf([]byte("other bytes")); got != want {
		t.Fatalf("the store's value = %s, want %s", got, want)
	}
}

func TestWriterChecksumOffWhenTheSettingIsOff(t *testing.T) {
	payload := []byte("the checkpoint as sent")
	store := &fakeStore{report: func(received []byte) (string, string) {
		return encodeCRC32C(received), "FULL_OBJECT"
	}}
	server := httptest.NewServer(store)
	t.Cleanup(server.Close)
	client := s3.New(s3.Options{
		Region:       "us-east-1",
		Credentials:  credentials.NewStaticCredentialsProvider("key", "secret", ""),
		BaseEndpoint: aws.String(server.URL),
		UsePathStyle: true,
	})
	cedana_config.Global.Checkpoint.Checksum = false
	t.Cleanup(func() { cedana_config.Global.Checkpoint.Checksum = true })

	file := NewFile(context.Background(), client, "bucket", "checkpoint.tar.lz4")
	if _, err := file.Write(payload); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if file.Checksum() != "" {
		t.Fatalf("checksum = %s, want none", file.Checksum())
	}
}
