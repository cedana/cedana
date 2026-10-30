package s3

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"

	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	cedana_config "github.com/cedana/cedana/pkg/config"
	cedana_io "github.com/cedana/cedana/pkg/io"
	"github.com/cedana/cedana/pkg/utils"
	"github.com/rs/zerolog/log"
)

const MULTIPART_SIZE = 5 * utils.MEBIBYTE

type File struct {
	ctx    context.Context
	client *s3.Client
	bucket string
	key    string

	reader io.ReadCloser
	writer io.WriteCloser
	done   chan error

	// The checksum of the object as stored, "crc32c:<hex>", known after Close:
	// the writer's own value over the bytes sent. The store's whole-object
	// value, when it gave one, is compared with it and a difference logged
	hasher   *cedana_io.ChecksumWriter
	stored   string // the store's value, from the completed upload
	checksum string
}

func NewFile(ctx context.Context, client *s3.Client, bucket, key string) *File {
	return &File{ctx: ctx, client: client, bucket: bucket, key: key}
}

func (c *File) Read(p []byte) (int, error) {
	if c.reader == nil {
		resp, err := c.client.GetObject(
			c.ctx, &s3.GetObjectInput{
				Bucket: &c.bucket,
				Key:    &c.key,
			},
		)
		if err != nil {
			var noKey *types.NoSuchKey
			if errors.As(err, &noKey) {
				return 0, fmt.Errorf("%s/%s does not exist", c.bucket, c.key)
			} else {
				return 0, fmt.Errorf("failed to get object %s/%s: %w", c.bucket, c.key, err)
			}
		}
		c.reader = resp.Body
	}
	return c.reader.Read(p)
}

func (c *File) Write(p []byte) (int, error) {
	if c.writer == nil {
		pr, pw := io.Pipe()

		c.writer = pw
		if cedana_config.Global.Checkpoint.Checksum {
			// The writer's own value over the bytes sent, which is the checksum
			c.hasher = cedana_io.NewChecksumWriter(pw)
			c.writer = c.hasher
		}
		c.done = make(chan error, 1)

		go func() {
			defer close(c.done)
			defer pr.Close()

			uploader := manager.NewUploader(c.client, func(u *manager.Uploader) {
				u.PartSize = MULTIPART_SIZE
			})

			input := &s3.PutObjectInput{
				Bucket: &c.bucket,
				Key:    &c.key,
				Body:   pr,
			}
			if c.hasher != nil {
				// The store computes the CRC32C of the object as it receives it, which is
				// read back beside the writer's value. The uploader of this SDK version
				// cannot ask for the whole-object type on a multipart upload, so a large
				// object comes back with a composite value, which is not the checksum of
				// the bytes and is not compared
				input.ChecksumAlgorithm = types.ChecksumAlgorithmCrc32c
			}
			out, err := uploader.Upload(c.ctx, input)
			if err != nil {
				c.done <- fmt.Errorf("failed to upload object %s/%s: %w", c.bucket, c.key, err)
				return
			}
			if out != nil && out.ChecksumCRC32C != nil && out.ChecksumType != types.ChecksumTypeComposite {
				c.stored = decodeCRC32C(*out.ChecksumCRC32C)
			}
		}()
	}

	return c.writer.Write(p)
}

// decodeCRC32C turns the store's base64 of the four CRC32C bytes into the format
// every checksum here has, "crc32c:<hex>"; "" if it is not four bytes
func decodeCRC32C(encoded string) string {
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(raw) != 4 {
		return ""
	}
	return cedana_io.FormatChecksum(uint32(raw[0])<<24 | uint32(raw[1])<<16 | uint32(raw[2])<<8 | uint32(raw[3]))
}

// Checksum returns the checksum of the uploaded object as stored, after Close
func (c *File) Checksum() string {
	return c.checksum
}

func (c *File) Close() error {
	var err error

	if c.reader != nil {
		err = errors.Join(err, c.reader.Close())
	}
	if c.writer != nil {
		err = errors.Join(err, c.writer.Close())
	}
	if c.done != nil {
		err = errors.Join(err, <-c.done)
	}
	if err == nil && c.hasher != nil {
		c.checksum = c.hasher.Sum()
		if c.stored != "" && c.stored != c.checksum {
			// The store holds other bytes than those sent. The value of the bytes sent
			// is recorded all the same; the check that fails the upload comes later
			log.Warn().Str("bucket", c.bucket).Str("key", c.key).Str("stored", c.stored).Str("sent", c.checksum).
				Msg("checksum of the object as stored differs from the bytes sent")
		}
	}
	c.client = nil

	return err
}
