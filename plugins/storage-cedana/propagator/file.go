package propagator

import (
	"fmt"
	"io"
	"net/http"
)

// Cedana managed file
type File struct {
	DownloadURL string
	UploadURL   string

	reader io.ReadCloser
	writer io.WriteCloser
	pipeW  *io.PipeWriter
	resp   *http.Response
}

func NewFile(downloadUrl, uploadUrl string) *File {
	return &File{DownloadURL: downloadUrl, UploadURL: uploadUrl}
}

// Read streams data from the download URL
func (c *File) Read(p []byte) (int, error) {
	if c.reader == nil {
		resp, err := http.Get(c.DownloadURL)
		if err != nil {
			return 0, err
		}
		if resp.StatusCode != http.StatusOK {
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
		c.pipeW = pw
		c.writer = pw

		req, err := http.NewRequest("PUT", c.UploadURL, pr)
		if err != nil {
			return 0, err
		}
		req.Header.Set("Content-Type", "application/octet-stream")

		go func() {
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				pw.CloseWithError(err)
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode < 200 || resp.StatusCode >= 300 {
				pw.CloseWithError(fmt.Errorf("upload failed: %s", resp.Status))
				return
			}
		}()
	}

	return c.writer.Write(p)
}

// Close closes underlying readers/writers
func (c *File) Close() error {
	var err error

	if c.reader != nil {
		err = c.reader.Close()
		c.reader = nil
	}
	if c.pipeW != nil {
		err = c.pipeW.Close()
		c.pipeW = nil
	}

	return err
}
