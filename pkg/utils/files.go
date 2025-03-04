package utils

import (
	"archive/tar"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	cedana_io "github.com/cedana/cedana/pkg/io"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

const (
	BYTE = 1.0 << (10 * iota)
	KIBIBYTE
	MEBIBYTE
	GIBIBYTE
)

// Tar creates a tarball from the provided sources and writes it to the destination.
// The desination should be a path without any file extension, as the function will add extension
// based on the compression format specified.
// FIXME: Works only with files, not directories in the tarball.
func Tar(src string, tarball string, compression string) (string, error) {
	ext, err := cedana_io.ExtForCompression(compression)
	if err != nil {
		return "", err
	}

	tarball += ".tar" + ext

	file, err := os.Create(tarball)
	if err != nil {
		return "", fmt.Errorf("Could not create tarball file: %s", err)
	}
	defer file.Close()
	defer func() {
		if err != nil {
			os.Remove(tarball)
		}
	}()

	writer, err := cedana_io.NewCompressionWriter(file, compression)
	if err != nil {
		return "", err
	}
	defer writer.Close()

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
// FIXME: Works only with files, not directories in the tarball.
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

	compression, err := cedana_io.CompressionFromExt(tarball)
	if err != nil {
		return err
	}

	reader, err := cedana_io.NewCompressionReader(file, compression)
	if err != nil {
		return err
	}
	defer reader.Close()

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

// WriteTo writes the contents from the provided src to the the provided target file.
// Compression format is specified by the compression argument.
// If the src is a pipe:
// 1. No compression: WIP
// 2. With compression: WIP
func WriteTo(src *os.File, target string, compression string) (int64, error) {
	defer src.Close()

	ext, err := cedana_io.ExtForCompression(compression)
	if err != nil {
		return 0, err
	}

	target += ext

	file, err := os.Create(target)
	if err != nil {
		return 0, err
	}
	defer file.Close()

	writer, err := cedana_io.NewCompressionWriter(file, compression)
	if err != nil {
		return 0, err
	}
	defer writer.Close()

	return src.WriteTo(writer)
}

// WriteToS3 writes the contents from the provided src to the specified S3 bucket and key.
// Uses Transfer Manager for streaming uploads, avoiding Content-Length requirement.
func WriteToS3(
	ctx context.Context,
	s3Client *s3.Client,
	source *os.File,
	bucket, key, compression string) (int64, error) {
	defer source.Close()

	pr, pw := io.Pipe()

	var written int64
	go func() {
		defer pw.Close()
		writer, err := cedana_io.NewCompressionWriter(pw, compression)
		if err != nil {
			pw.CloseWithError(err)
			return
		}
		defer writer.Close()
		n, err := io.Copy(writer, source)
		if err != nil {
			pw.CloseWithError(err)
			return
		}
		written = n
	}()

	// Create the upload manager with Transfer Manager
	uploader := manager.NewUploader(s3Client, func(u *manager.Uploader) {
		u.PartSize = 5 * 1024 * 1024 // 5MB part size
		u.Concurrency = 3            // number of concurrent uploads
		u.LeavePartsOnError = false  // Clean up parts if upload fails
	})

	_, err := uploader.Upload(ctx, &s3.PutObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
		Body:   pr,
	})
	if err != nil {
		return 0, fmt.Errorf("failed to upload to S3: %w", err)
	}

	return written, nil
}

// ReadFrom reads the contents of the src file and writes it to the provided target.
// The function automatically detects the compression format from the file extension.
// If the target is a pipe:
// 1. No compression: Uses the splice() system call to move the data avoiding kernel-user space copy.
// 2. With compression: WIP
func ReadFrom(src string, target *os.File) (int64, error) {
	defer target.Close()

	file, err := os.Open(src)
	if err != nil {
		return 0, err
	}
	defer file.Close()

	compression, err := cedana_io.CompressionFromExt(src)
	if err != nil {
		return 0, err
	}

	if compression == "none" {
		isPipe, err := cedana_io.IsPipe(target.Fd())
		if err != nil {
			return 0, err
		}
		if isPipe {
			return cedana_io.Splice(file.Fd(), target.Fd())
		}
	}

	reader, err := cedana_io.NewCompressionReader(file, compression)
	if err != nil {
		return 0, err
	}
	defer reader.Close()

	return target.ReadFrom(reader)
}

func ReadFromS3(
	ctx context.Context,
	s3Client *s3.Client,
	bucket, key string,
	target *os.File,
	compression string) (int64, error) {
	defer target.Close()

	resp, err := s3Client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return 0, fmt.Errorf("failed to get object from S3: %w", err)
	}
	defer resp.Body.Close()

	// Decompress the stream based on compression format
	reader, err := cedana_io.NewCompressionReader(resp.Body, compression)
	if err != nil {
		return 0, err
	}
	defer reader.Close()

	isPipe, err := cedana_io.IsPipe(target.Fd())
	if err != nil {
		return 0, err
	}

	if compression == "none" && isPipe {
		return io.Copy(target, reader)
	} else {
		return io.Copy(target, reader)
	}
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

func ChownAll(path string, uid, gid int) error {
	return filepath.Walk(path, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return fmt.Errorf("filepath.Walk() failed: %s", err)
		}
		return os.Chown(path, uid, gid)
	})
}
