package utils

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	BYTE = 1.0 << (10 * iota)
	KIBIBYTE
	MEBIBYTE
	GIBIBYTE
)

func ListFilesInDir(dir string) ([]string, error) {
	var files []string

	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return fmt.Errorf("filepath.Walk() failed: %s", err)
		}
		if !info.IsDir() {
			files = append(files, path)
		}
		return nil
	})

	return files, err
}

func Kibibytes(bytes int64) int64 {
	return bytes / 1024
}

func Mebibytes(bytes int64) int64 {
	return bytes / 1024 / 1024
}

func Gibibytes(bytes int64) int64 {
	return bytes / 1024 / 1024 / 1024
}

// SizeFromPath returns the size of the file or directory at the provided path.
func SizeFromPath(path string) int64 {
	var size int64
	err := filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			size += info.Size()
		}
		return err
	})
	if err != nil {
		return 0
	}
	return size
}

func SizeStr(bytes int64) string {
	if bytes <= 0 {
		return ""
	}
	unit := ""
	value := float64(bytes)

	switch {
	case bytes >= GIBIBYTE:
		unit = "GiB"
		value = value / GIBIBYTE
	case bytes >= MEBIBYTE:
		unit = "MiB"
		value = value / MEBIBYTE
	case bytes >= KIBIBYTE:
		unit = "KiB"
		value = value / KIBIBYTE
	case bytes >= BYTE:
		unit = "bytes"
	case bytes == 0:
		return "0"
	}

	stringValue := strings.TrimSuffix(
		fmt.Sprintf("%.0f", value), ".00",
	)

	return fmt.Sprintf("%s %s", stringValue, unit)
}

func CopyFile(src, dest string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("Could not open source file: %s", err)
	}
	defer srcFile.Close()

	destFile, err := os.Create(dest)
	if err != nil {
		return fmt.Errorf("Could not create destination file: %s", err)
	}
	defer destFile.Close()

	// Set the file permissions to the source file's permissions
	if srcFileInfo, err := os.Stat(src); err == nil {
		err = destFile.Chmod(srcFileInfo.Mode())
		if err != nil {
			return fmt.Errorf("Could not set destination file permissions: %s", err)
		}
	} else {
		return fmt.Errorf("Could not get source file info: %s", err)
	}

	_, err = io.Copy(destFile, srcFile)
	if err != nil {
		return fmt.Errorf("Could not copy file contents: %s", err)
	}

	return nil
}

func PathExists(path string) bool {
	_, err := os.Stat(path)
	return !os.IsNotExist(err)
}

func WaitForFile[T any](ctx context.Context, path string, timeout <-chan T) ([]byte, error) {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
			if PathExists(path) {
				return os.ReadFile(path)
			}
		case <-timeout:
			return nil, fmt.Errorf("timed out waiting for %s", path)
		}
	}
}

func ChownAll(path string, uid, gid int) error {
	return filepath.Walk(path, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return fmt.Errorf("filepath.Walk() failed: %s", err)
		}
		return os.Chown(path, uid, gid)
	})
}
