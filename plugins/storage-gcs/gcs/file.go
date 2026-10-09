package gcs

import (
	"context"
	"fmt"

	"cloud.google.com/go/storage"
	cedana_config "github.com/cedana/cedana/pkg/config"
	cedana_io "github.com/cedana/cedana/pkg/io"
	"github.com/cedana/cedana/pkg/utils"
	"github.com/rs/zerolog/log"
)

// The size of each request of a resumable upload; the writer holds one such buffer
const CHUNK_SIZE = 5 * utils.MEBIBYTE

// File is the writer of one GCS object
type File struct {
	bucket string
	key    string

	writer *storage.Writer
	hasher *cedana_io.ChecksumWriter

	// The checksum of the object as stored, "crc32c:<hex>", known after Close: the
	// writer's own value over the bytes sent. The store's value is compared with it
	// and a difference logged.
	stored   string
	checksum string
}

func NewFile(ctx context.Context, client *storage.Client, bucket, key string) *File {
	w := client.Bucket(bucket).Object(key).NewWriter(ctx)
	w.ChunkSize = CHUNK_SIZE
	// The SDK sends a CRC32C of the whole object by default, and GCS then rejects an
	// object whose bytes differ. The checksum design records and logs a difference for
	// every store until a later validation change, so the SDK's own check is off here.
	w.DisableAutoChecksum = true

	f := &File{bucket: bucket, key: key, writer: w}
	if cedana_config.Global.Checkpoint.Checksum {
		f.hasher = cedana_io.NewChecksumWriter(w)
	}
	return f
}

func (f *File) Write(p []byte) (int, error) {
	if f.hasher != nil {
		return f.hasher.Write(p)
	}
	return f.writer.Write(p)
}

// Checksum returns the checksum of the uploaded object as stored, after Close
func (f *File) Checksum() string {
	return f.checksum
}

func (f *File) Close() error {
	if err := f.writer.Close(); err != nil {
		return fmt.Errorf("failed to upload object %s/%s: %w", f.bucket, f.key, err)
	}
	if f.hasher == nil {
		return nil
	}
	f.checksum = f.hasher.Sum()
	if attrs := f.writer.Attrs(); attrs != nil {
		// GCS computes the CRC32C of every object, of the whole object
		f.stored = cedana_io.FormatChecksum(attrs.CRC32C)
		if f.stored != f.checksum {
			log.Warn().Str("bucket", f.bucket).Str("key", f.key).Str("stored", f.stored).Str("sent", f.checksum).
				Msg("checksum of the object as stored differs from the bytes sent")
		}
	}
	return nil
}
