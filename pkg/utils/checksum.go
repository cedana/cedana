package utils

import (
	"crypto/md5"
	"fmt"
	"io"
	"os"
)

// Computes the MD5 checksum of the given files (read in order)
func FileMD5Sum(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	h := md5.New()

	if _, err := io.Copy(h, file); err != nil {
		return "", err
	}

	return fmt.Sprintf("%x", h.Sum(nil)), nil
}
