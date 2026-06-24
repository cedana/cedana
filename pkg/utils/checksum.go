package utils

import (
	"fmt"
	"hash/crc32"
	"io"
	"os"
)

// Computes the CRC32 checksum of the given file.
func FileCRC32Sum(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	h := crc32.NewIEEE()

	if _, err := io.Copy(h, file); err != nil {
		return "", err
	}

	return fmt.Sprintf("%08x", h.Sum32()), nil
}
