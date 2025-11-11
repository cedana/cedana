package io

import (
	"compress/gzip"
	"compress/zlib"
	"fmt"
	"io"
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
