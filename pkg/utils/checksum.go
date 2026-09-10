package utils

import (
	"encoding/base64"
	"encoding/binary"
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
	sum := h.Sum32()
	buf := make([]byte, 4)
	binary.BigEndian.PutUint32(buf, sum)

	return base64.StdEncoding.EncodeToString(buf), nil
}
