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

const (
	BYTE = 1.0 << (10 * iota)
	KIBIBYTE
	MEBIBYTE
	GIBIBYTE
)

type FdInfo struct {
	Pos   int
	Flags int
	MntId int
	Inode int
}

// CreateTarball creates a tarball from the provided sources and writes it to the destination.
// The desination should be a path without any file extension, as the function will add extension
// based on the compression format specified.
// XXX: Works only with files, not directories.
func Tar(src string, tarball string, compression string) (string, error) {
	switch compression {
	case "gzip", "gz":
		tarball += ".tar.gz"
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
	case "gzip", "gz":
		writer = gzip.NewWriter(file)
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

// DecompressTarball decompresses the provided tarball to the destination directory.
// The destination directory should already exist.
// The function automatically detects the compression format from the file extension.
// XXX: Works only with files, not directories.
func Untar(tarball string, dest string) error {
	file, err := os.Open(tarball)
	if err != nil {
		return fmt.Errorf("Could not open tarball file: %s", err)
	}
	defer file.Close()

	var reader io.ReadCloser

	var compression string

	switch filepath.Ext(tarball) {
	case ".gz":
		compression = "gzip"
	case ".tar":
		compression = "none"
	default:
		return fmt.Errorf("Unsupported compression format: %s", compression)
	}

	if compression == "gzip" {
		reader, err = gzip.NewReader(file)
		if err != nil {
			return fmt.Errorf("Could not create gzip reader: %s", err)
		}
		defer reader.Close()
	} else {
		reader = file
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
			if err := os.MkdirAll(target, 0755); err != nil {
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

// FdInfo returns file descriptor information for the provided process and file descriptor.
func GetFdInfo(pid uint32, fd int) (*FdInfo, error) {
	path := fmt.Sprintf("/proc/%d/fdinfo/%d", pid, fd)
	contents, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("Could not read fdinfo file: %s", err)
	}

	// Parse the fdinfo file
	var info FdInfo
	lines := strings.Split(string(contents), "\n")
	for _, line := range lines {
		parts := strings.Split(line, ":")
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		switch key {
		case "pos":
			fmt.Sscanf(value, "%d", &info.Pos)
		case "flags":
			fmt.Sscanf(value, "%d", &info.Flags)
		case "mnt_id":
			fmt.Sscanf(value, "%d", &info.MntId)
		case "ino":
			fmt.Sscanf(value, "%d", &info.Inode)
		}
	}

	return &info, nil
}
