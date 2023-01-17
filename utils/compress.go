package utils

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// https://github.com/mimoo/eureka/blob/master/folders.go

func Compress(src string, buf io.Writer) error {
	zr := gzip.NewWriter(buf)
	tw := tar.NewWriter(zr)

	fi, err := os.Stat(src)
	if err != nil {
		return err
	}

	mode := fi.Mode()

	if mode.IsDir() {
		filepath.Walk(src, func(file string, fi os.FileInfo, err error) error {
			if err != nil {
				return fmt.Errorf("could not traverse path %s", src)
			}
			header, err := tar.FileInfoHeader(fi, file)
			if err != nil {
				return err
			}

			// (see https://golang.org/src/archive/tar/common.go?#L626)
			// these need to be relative paths
			header.Name = filepath.Join(filepath.Base(src), filepath.Base(file))
			if err := tw.WriteHeader(header); err != nil {
				return err
			}

			if !fi.IsDir() {
				data, err := os.Open(file)
				if err != nil {
					return err
				}
				if _, err := io.Copy(tw, data); err != nil {
					return err
				}
			}

			return nil
		})
	} else {
		return fmt.Errorf("filetype not supported")
	}

	if err := tw.Close(); err != nil {
		return err
	}

	return nil
}

func Decompress(src io.Reader, dst string) error {
	zr, err := gzip.NewReader(src)
	if err != nil {
		return err
	}

	tr := tar.NewReader(zr)

	// uncompress each element
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		target := header.Name

		if !validRelPath(target) {
			return fmt.Errorf("tar contained invalid name error %q", target)
		}

		// append to path to destination
		target = filepath.Join(dst, header.Name)

		switch header.Typeflag {
		// if not dir, create
		case tar.TypeDir:
			if _, err := os.Stat(target); err != nil {
				if err := os.MkdirAll(target, 0o755); err != nil {
					return err
				}
			}
		case tar.TypeReg:
			fileToWrite, err := os.OpenFile(target, os.O_CREATE|os.O_RDWR, os.FileMode(header.Mode))
			if err != nil {
				return err
			}
			if _, err := io.Copy(fileToWrite, tr); err != nil {
				return err
			}

			fileToWrite.Close()
		}
	}

	return nil
}

// check for path traversal and correct forward slashes
func validRelPath(p string) bool {
	if p == "" || strings.Contains(p, `\`) || strings.HasPrefix(p, "/") || strings.Contains(p, "../") {
		return false
	}
	return true
}
