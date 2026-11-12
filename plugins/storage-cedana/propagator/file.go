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
	// Uploads are spooled to a temp file and sent on Close: the presigned S3 PUT
	// needs a Content-Length, which a streamed (chunked) body cannot provide.
	spool *os.File
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
	if c.spool == nil {
		f, err := os.CreateTemp("", "cedana-upload-*")
		if err != nil {
			return 0, fmt.Errorf("failed to create upload spool: %w", err)
		}
		c.spool = f
	}

	return c.spool.Write(p)
}

func (c *File) Close() error {
	var err error

	if c.reader != nil {
		err = errors.Join(err, c.reader.Close())
	}
	if c.spool != nil {
		err = errors.Join(err, c.upload())
	}

	return err
}

// upload sends the spooled bytes with a known Content-Length and removes the spool.
func (c *File) upload() error {
	defer os.Remove(c.spool.Name())
	defer c.spool.Close()

	size, err := c.spool.Seek(0, io.SeekCurrent)
	if err != nil {
		return err
	}
	if _, err := c.spool.Seek(0, io.SeekStart); err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(c.ctx, "PUT", c.uploadURL, c.spool)
	if err != nil {
		return err
	}
	req.ContentLength = size

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("upload failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("upload failed with status: %s", resp.Status)
	}
	return nil
}
