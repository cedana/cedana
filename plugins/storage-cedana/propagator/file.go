package propagator

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
)

// Cedana managed file
type File struct {
	ctx         context.Context
	downloadURL string
	uploadURL   string

	reader io.ReadCloser
	writer io.WriteCloser
	done   chan error
}

func NewDownloadableFile(ctx context.Context, downloadUrl string) *File {
	return &File{ctx: ctx, downloadURL: downloadUrl}
}

func NewUploadableFile(ctx context.Context, uploadUrl string) *File {
	return &File{ctx: ctx, uploadURL: uploadUrl}
}

func (c *File) Read(p []byte) (int, error) {
	if c.reader == nil {
		req, err := http.NewRequestWithContext(c.ctx, "GET", c.downloadURL, nil)
		if err != nil {
			return 0, err
		}

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return 0, err
		}

		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return 0, fmt.Errorf("failed to download: %s", resp.Status)
		}
		c.reader = resp.Body
	}
	return c.reader.Read(p)
}

func (c *File) Write(p []byte) (int, error) {
	if c.writer == nil {
		pr, pw, err := os.Pipe()
		if err != nil {
			return 0, err
		}

		c.writer = pw
		c.done = make(chan error, 1)

		req, err := http.NewRequestWithContext(c.ctx, "PUT", c.uploadURL, pr)
		if err != nil {
			return 0, err
		}

		go func() {
			defer close(c.done)

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				c.done <- fmt.Errorf("upload failed: %w", err)
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode < 200 || resp.StatusCode >= 300 {
				c.done <- fmt.Errorf("upload failed with status: %s", resp.Status)
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

	return err
}
