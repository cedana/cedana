package propagator

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Presigned S3 PUTs need a Content-Length: a streamed (chunked) upload is rejected
func TestUploadSendsContentLength(t *testing.T) {
	var got []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ContentLength < 0 || len(r.TransferEncoding) > 0 {
			http.Error(w, "MissingContentLength", http.StatusLengthRequired)
			return
		}
		got, _ = io.ReadAll(r.Body)
	}))
	defer server.Close()

	want := bytes.Repeat([]byte("checkpoint"), 100_000)
	f := NewUploadableFile(context.Background(), server.URL)
	for rest := want; len(rest) > 0; {
		n := min(64<<10, len(rest))
		if _, err := f.Write(rest[:n]); err != nil {
			t.Fatalf("write: %v", err)
		}
		rest = rest[n:]
	}
	if err := f.Close(); err != nil {
		t.Fatalf("upload failed: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("uploaded %d bytes, want %d", len(got), len(want))
	}
}

// The upload happens on Close, so a failed upload must be Close's error, not lost
func TestUploadFailureIsReturnedByClose(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		http.Error(w, "InternalError", http.StatusInternalServerError)
	}))
	defer server.Close()

	f := NewUploadableFile(context.Background(), server.URL)
	if _, err := f.Write([]byte("checkpoint")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := f.Close(); err == nil {
		t.Error("Close returned no error for a failed upload")
	}
}
