package propagator

import (
	"fmt"
	"io"
	"net/http"

	"github.com/rs/zerolog/log"
)

// Cedana managed file
type File struct {
	DownloadURL string
	UploadURL   string

	reader io.ReadCloser
	writer *io.PipeWriter
	resp   *http.Response
	done   chan error
}

func NewFile(downloadUrl, uploadUrl string) *File {
	return &File{DownloadURL: downloadUrl, UploadURL: uploadUrl}
}

// Read streams data from the download URL
func (c *File) Read(p []byte) (int, error) {
	if c.reader == nil {
		resp, err := http.Get(c.DownloadURL)
		if err != nil {
      log.Error().Err(err).Msg("failed to download file")
			return 0, err
		}
		if resp.StatusCode != http.StatusOK {
      log.Error().Str("status", resp.Status).Msg("failed to download file")
			resp.Body.Close()
			return 0, fmt.Errorf("failed to download: %s", resp.Status)
		}
		c.reader = resp.Body
		c.resp = resp
	}
	return c.reader.Read(p)
}

// Write streams data to the upload URL using io.Pipe
func (c *File) Write(p []byte) (int, error) {
	if c.writer == nil {
		pr, pw := io.Pipe()
		c.writer = pw
		c.done = make(chan error, 1)

		req, err := http.NewRequest("PUT", c.UploadURL, pr)
		if err != nil {
			return 0, err
		}

		go func() {
			defer close(c.done)

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				c.done <- fmt.Errorf("upload failed: %w", err)
				pw.CloseWithError(err)
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode < 200 || resp.StatusCode >= 300 {
				c.done <- fmt.Errorf("upload failed with status: %s", resp.Status)
				pw.CloseWithError(fmt.Errorf("upload failed with status: %s", resp.Status))
				return
			}
		}()
	}

	return c.writer.Write(p)
}

func (c *File) Close() error {
	var err error

	if c.reader != nil {
		if e := c.reader.Close(); e != nil {
			err = e
		}
		c.reader = nil
	}
	if c.writer != nil {
		if e := c.writer.Close(); e != nil && err == nil {
			err = e
		}
		c.writer = nil
	}
	if c.done != nil {
		if e := <-c.done; e != nil && err == nil {
			err = e
		}
		c.done = nil
	}

	return err
}
