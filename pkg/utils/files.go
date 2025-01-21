package utils

import (
	"archive/tar"
	"compress/gzip"
	"compress/zlib"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pierrec/lz4"
)

const (
	BYTE = 1.0 << (10 * iota)
	KIBIBYTE
	MEBIBYTE
	GIBIBYTE
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

// Tar creates a tarball from the provided sources and writes it to the destination.
// The desination should be a path without any file extension, as the function will add extension
// based on the compression format specified.
// XXX: Works only with files, not directories.
func Tar(src string, tarball string, compression string) (string, error) {
	switch compression {
	case "lz4":
		tarball += ".tar.lz4"
	case "gzip", "gz":
		tarball += ".tar.gz"
	case "zlib":
		tarball += ".tar.zlib"
	case "tar":
		tarball += ".tar"
	case "", "none":
		tarball += ".tar"
	default:
		return "", fmt.Errorf("Unsupported compression format: %s", compression)
	}

	file, err := os.Create(tarball)
	if err != nil {
		return "", fmt.Errorf("Could not create tarball file: %s", err)
	}
	defer file.Close()

	var writer io.WriteCloser

	switch compression {
	case "lz4":
		writer = lz4.NewWriter(file)
		defer writer.Close()
	case "gzip", "gz":
		writer = gzip.NewWriter(file)
		defer writer.Close()
	case "zlib":
		writer = zlib.NewWriter(file)
		defer writer.Close()
	case "tar":
		writer = file
	case "", "none":
		writer = file
	default:
		os.Remove(tarball)
		return "", fmt.Errorf("Unsupported compression format: %s", compression)
	}

	tarWriter := tar.NewWriter(writer)
	defer tarWriter.Close()

	err = filepath.Walk(src, func(file string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		header, err := tar.FileInfoHeader(fi, file)
		if err != nil {
			return err
		}

		// Adjust the file's path to exclude the base directory
		relPath, err := filepath.Rel(src, file)
		if err != nil {
			return err
		}
		header.Name = relPath

		if err := tarWriter.WriteHeader(header); err != nil {
			return err
		}

		if !fi.Mode().IsRegular() {
			return nil
		}

		srcFile, err := os.Open(file)
		if err != nil {
			return err
		}
		defer srcFile.Close()

		_, err = io.Copy(tarWriter, srcFile)
		return err
	})

	return tarball, nil
}

// Untar decompresses the provided tarball to the destination directory.
// The destination directory should already exist.
// The function automatically detects the compression format from the file extension.
// XXX: Works only with files, not directories.
func Untar(tarball string, dest string) error {
	file, err := os.Open(tarball)
	if err != nil {
		return fmt.Errorf("Could not open tarball file: %s", err)
	}
	defer file.Close()

	stat, err := file.Stat()
	if err != nil {
		return fmt.Errorf("Could not get file stats: %s", err)
	}
	perm := stat.Mode().Perm()

	var reader io.Reader

	switch filepath.Ext(tarball) {
	case ".lz4":
		reader = lz4.NewReader(file)
	case ".gz":
		readCloser, err := gzip.NewReader(file)
		if err != nil {
			return fmt.Errorf("Could not create gzip reader: %s", err)
		}
		defer readCloser.Close()
		reader = readCloser
	case ".zlib":
		readCloser, err := zlib.NewReader(file)
		if err != nil {
			return fmt.Errorf("Could not create zlib reader: %s", err)
		}
		defer readCloser.Close()
		reader = readCloser
	case ".tar":
		reader = file
	default:
		return fmt.Errorf("Unsupported compression format: %s", strings.TrimPrefix(filepath.Ext(tarball), "."))
	}

	tarReader := tar.NewReader(reader)

	// Iterate through the files in the tarball
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break // End of tarball
		}
		if err != nil {
			return err
		}

		// Clean and validate the path
		cleanedPath := filepath.Clean(header.Name)
		if strings.Contains(cleanedPath, "..") {
			return fmt.Errorf("invalid file path: %s", cleanedPath)
		}

		// Construct the full path for the file
		target := filepath.Join(dest, cleanedPath)

		// Check the type of the file
		switch header.Typeflag {
		case tar.TypeDir:
			// Create directory
			if err := os.MkdirAll(target, perm); err != nil {
				return err
			}
		case tar.TypeReg:
			// Create file and write data into it
			outFile, err := os.Create(target)
			if err != nil {
				return err
			}

			if _, err := io.Copy(outFile, tarReader); err != nil {
				outFile.Close()
				return err
			}
			outFile.Close()
		}
	}

	return nil
}

// WriteTo writes the contents from the provided file to the the provided path.
// Compression format is specified by the compression argument.
func WriteTo(src *os.File, path string, compression string) error {
	defer src.Close()
	switch compression {
	case "lz4":
		path += ".lz4"
	case "gzip", "gz":
		path += ".gz"
	case "zlib":
		path += ".zlib"
	case "", "none", "tar": // Taring single file makes no sense, so for compatibility, we just write as file
		// Do nothing
	default:
		return fmt.Errorf("Unsupported compression format: %s", compression)
	}

	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("Could not create file: %s", err)
	}

	var writer io.WriteCloser
	switch compression {
	case "lz4":
		writer = lz4.NewWriter(file)
		defer writer.Close()
	case "gzip", "gz":
		writer = gzip.NewWriter(file)
		defer writer.Close()
	case "zlib":
		writer = zlib.NewWriter(file)
		defer writer.Close()
	case "", "none", "tar": // Taring single file makes no sense, so for compatibility, we just write as file
		writer = file
	default:
		os.Remove(path)
		return fmt.Errorf("Unsupported compression format: %s", compression)
	}

	_, err = src.WriteTo(writer)
	return err
}

// ReadFrom reads the contents of the file at the provided path and writes it to the provided file.
// The function automatically detects the compression format from the file extension.
func ReadFrom(path string, target *os.File) error {
	defer target.Close()
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("Could not open file: %s", err)
	}
	defer file.Close()

	var reader io.Reader
	switch filepath.Ext(path) {
	case ".lz4":
		reader = lz4.NewReader(file)
	case ".gz":
		readCloser, err := gzip.NewReader(file)
		if err != nil {
			return fmt.Errorf("Could not create gzip reader: %s", err)
		}
		defer readCloser.Close()
		reader = readCloser
	case ".zlib":
		readCloser, err := zlib.NewReader(file)
		if err != nil {
			return fmt.Errorf("Could not create zlib reader: %s", err)
		}
		defer readCloser.Close()
		reader = readCloser
	case "": // Taring single file makes no sense, so for compatibility, we just write as file
		reader = file
	default:
		return fmt.Errorf("Unsupported compression format: %s", strings.TrimPrefix(filepath.Ext(path), "."))
	}

	_, err = target.ReadFrom(reader)
	return err
}

//////////////////////////
//// Helper Functions ////
//////////////////////////

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
func SizeFromPath(path string) (int64, error) {
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
	return size, err
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

func WaitForFile(ctx context.Context, path string, timeout chan int) (string, error) {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-ticker.C:
			if PathExists(path) {
				return path, nil
			}
		case <-timeout:
			return "", fmt.Errorf("timed out waiting for %s", path)
		}
	}
}
