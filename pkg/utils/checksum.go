package utils

import (
	"crypto/md5"
	"io"
	"os"
)

// Computes the MD5 checksum of the given files (read in order)
func FileMD5Sum(paths ...string) ([]byte, error) {
	h := md5.New()

	for _, path := range paths {
		file, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		defer file.Close()

		if _, err := io.Copy(h, file); err != nil {
			return nil, err
		}
	}

	return h.Sum(nil), nil
}
