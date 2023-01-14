package utils

import (
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
)

func compress(src string, buf io.Writer) error {
	zr := gzip.NewWriter(buf)

	fi, err := os.Stat(src)
	if err != nil {
		return err
	}

	mode := fi.Mode()

	if mode.IsDir() {
		filepath.Walk(src, func(file string, fi os.FileInfo, err error) error {

		})
	}
}
