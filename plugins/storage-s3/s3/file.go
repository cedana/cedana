package s3

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/cedana/cedana/pkg/utils"
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
		c.done = make(chan error, 1)

		go func() {
			defer close(c.done)
			defer pr.Close()

			uploader := manager.NewUploader(c.client, func(u *manager.Uploader) {
				u.PartSize = MULTIPART_SIZE
			})

			_, err := uploader.Upload(
				c.ctx, &s3.PutObjectInput{
					Bucket: &c.bucket,
					Key:    &c.key,
					Body:   pr,
				},
			)
			if err != nil {
				c.done <- fmt.Errorf("failed to upload object %s/%s: %w", c.bucket, c.key, err)
				return
			}
		}()
	}

	return c.writer.Write(p)
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
	c.client = nil

	return err
}
