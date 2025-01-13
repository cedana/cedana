package utils

import (
	"crypto/md5"
	"hash"
	"io"
	"os"
)

// Computes the MD5 checksum of the given files (read in order)
func FileMD5Sum(h hash.Hash, paths ...string) (hash.Hash, error) {
	if h == nil {
		h = md5.New()
	}

	for _, path := range paths {
		file, err := os.Open(path)
		if err != nil {
			return h, err
		}
		defer file.Close()

		if _, err := io.Copy(h, file); err != nil {
			return h, err
		}
	}
	return h, nil
}
