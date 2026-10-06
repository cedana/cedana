package cedana

import (
	"context"
	"errors"
	"testing"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/upload"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestWaitUpload(t *testing.T) {
	ctx := context.Background()
	server := &Server{Cedana: Cedana{uploads: upload.NewRegistry()}}

	t.Run("Succeeded", func(t *testing.T) {
		server.uploads.Start("s3://bucket/succeeded.tar")("crc32c:0000abcd", nil)

		resp, err := server.WaitUpload(ctx, &daemon.WaitUploadReq{Path: "s3://bucket/succeeded.tar"})
		if err != nil {
			t.Fatalf("wait failed: %v", err)
		}
		if resp.GetChecksum() != "crc32c:0000abcd" {
			t.Fatalf("expected the checksum of the upload, got %q", resp.GetChecksum())
		}
		if resp.GetError() != "" {
			t.Fatalf("unexpected response %v", resp)
		}
	})

	t.Run("Failed", func(t *testing.T) {
		server.uploads.Start("s3://bucket/failed.tar")("", errors.New("connection reset"))

		resp, err := server.WaitUpload(ctx, &daemon.WaitUploadReq{Path: "s3://bucket/failed.tar"})
		if err != nil {
			t.Fatalf("a failed upload must not fail the call: %v", err)
		}
		if resp.GetError() != "connection reset" {
			t.Fatalf("unexpected response %v", resp)
		}
	})

	t.Run("Unknown", func(t *testing.T) {
		_, err := server.WaitUpload(ctx, &daemon.WaitUploadReq{Path: "s3://bucket/unknown.tar"})
		if status.Code(err) != codes.NotFound {
			t.Fatalf("expected NotFound, got %v", err)
		}
	})

	t.Run("NoPath", func(t *testing.T) {
		_, err := server.WaitUpload(ctx, &daemon.WaitUploadReq{})
		if status.Code(err) != codes.InvalidArgument {
			t.Fatalf("expected InvalidArgument, got %v", err)
		}
	})

	t.Run("Canceled", func(t *testing.T) {
		server.uploads.Start("s3://bucket/running.tar")

		ctx, cancel := context.WithCancel(ctx)
		cancel()
		_, err := server.WaitUpload(ctx, &daemon.WaitUploadReq{Path: "s3://bucket/running.tar"})
		if status.Code(err) != codes.Canceled {
			t.Fatalf("expected Canceled, got %v", err)
		}
	})
}
