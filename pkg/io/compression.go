package io

import (
	"bufio"
	"compress/gzip"
	"compress/zlib"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/pierrec/lz4/v4"
)

var SUPPORTED_COMPRESSIONS = map[string]bool{
	"":     true,
	"none": true,
	"tar":  true,
	"gzip": true,
	"gz":   true,
	"lz4":  true,
	"zlib": true,
}

type AsyncVirtioWriter struct {
	file  *os.File
	pipeW *io.PipeWriter
	done  chan error
}

func NewAsyncVirtioWriter(f *os.File) *AsyncVirtioWriter {
	pr, pw := io.Pipe()
	a := &AsyncVirtioWriter{
		file:  f,
		pipeW: pw,
		done:  make(chan error, 1),
	}

	go func() {
		// large buffers for saturating virtio queue
		buf := make([]byte, 2*1024*1024)
		_, err := io.CopyBuffer(a.file, pr, buf)
		a.done <- err
	}()

	return a
}

func (a *AsyncVirtioWriter) Write(p []byte) (n int, err error) {
	return a.pipeW.Write(p)
}

func (a *AsyncVirtioWriter) Close() error {
	a.pipeW.Close()
	return <-a.done
}

func NewOptimizedCompressionWriter(baseWriter io.Writer, compression string, isFuse bool) (io.WriteCloser, error) {
	var targetWriter io.Writer = baseWriter
	var flushers []func() error

	if isFuse {
		bw := bufio.NewWriterSize(baseWriter, 1024*1024)
		targetWriter = bw
		flushers = append(flushers, bw.Flush)
	}

	var compressor io.WriteCloser
	switch compression {
	case "lz4":
		compressor = lz4.NewWriter(targetWriter)
	case "gzip", "gz":
		compressor = gzip.NewWriter(targetWriter)
	case "tar", "none", "":
		compressor = NopWriteCloser{targetWriter}
	default:
		return nil, fmt.Errorf("unsupported compression: %s", compression)
	}

	return &stackCloser{
		WriteCloser: compressor,
		flushers:    flushers,
	}, nil
}

type stackCloser struct {
	io.WriteCloser
	flushers []func() error
}

func (s *stackCloser) Close() error {
	// compression then flush
	err := s.WriteCloser.Close()
	for _, flush := range s.flushers {
		if fErr := flush(); fErr != nil && err == nil {
			err = fErr
		}
	}
	return err
}

func NewCompressionWriter(writer io.Writer, compression string) (io.WriteCloser, error) {
	switch compression {
	case "lz4":
		return lz4.NewWriter(writer), nil
	case "gzip", "gz":
		return gzip.NewWriter(writer), nil
	case "zlib":
		return zlib.NewWriter(writer), nil
	case "tar", "none", "":
		return NopWriteCloser{writer}, nil
	default:
		return nil, fmt.Errorf("Unsupported compression format: %s", compression)
	}
}

func NewCompressionReader(reader io.Reader, compression string) (io.ReadCloser, error) {
	switch compression {
	case "lz4":
		return NopReadCloser{lz4.NewReader(reader)}, nil
	case "gzip", "gz":
		return gzip.NewReader(reader)
	case "zlib":
		return zlib.NewReader(reader)
	case "tar", "none", "":
		return NopReadCloser{reader}, nil
	default:
		return nil, fmt.Errorf("Unsupported compression format: %s", compression)
	}
}

func CompressionFromExt(path string) (string, error) {
	ext := filepath.Ext(path)
	switch ext {
	case ".lz4":
		return "lz4", nil
	case ".gz", ".gzip":
		return "gzip", nil
	case ".zlib":
		return "zlib", nil
	case ".tar":
		return "tar", nil
	case "":
		return "none", nil
	default:
		return "", fmt.Errorf("Unsupported compression format: %s", ext)
	}
}

func ExtForCompression(compression string) (string, error) {
	switch compression {
	case "lz4":
		return ".lz4", nil
	case "gzip":
		return ".gz", nil
	case "zlib":
		return ".zlib", nil
	case "tar", "none", "":
		return "", nil
	default:
		return "", fmt.Errorf("Unsupported compression format: %s", compression)
	}
}

///////////////
/// Helpers ///
///////////////

type NopWriteCloser struct {
	io.Writer
}

type NopReadCloser struct {
	io.Reader
}

func (NopWriteCloser) Close() error {
	return nil
}

func (NopReadCloser) Close() error {
	return nil
}
