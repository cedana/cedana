package io

import (
	"archive/tar"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// CopyNotify asynchronously does io.Copy, notifying when done.
func CopyNotify(dst io.Writer, src io.Reader) chan error {
	done := make(chan error, 1)
	go func() {
		_, err := io.Copy(dst, src)
		done <- err
		close(done)
	}()
	return done
}

// Tar creates a tarball from the provided sources and writes it to the destination.
// FIXME: Works only with files, not directories in the tarball.
func Tar(src string, dst io.Writer, compression string) error {
	writer, err := NewCompressionWriter(dst, compression)
	if err != nil {
		return err
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

	return err
}

// Untar decompresses the provided tarball to the destination directory.
// The destination directory should already exist.
// FIXME: Works only with files, not directories in the tarball.
func Untar(src io.Reader, dest string, compression string) error {
	reader, err := NewCompressionReader(src, compression)
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
			if err := os.MkdirAll(target, os.ModePerm); err != nil {
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

// WriteTo writes the contents from the provided src to the the provided destination.
// Compression format is specified by the compression argument.
func WriteTo(src *os.File, dst io.Writer, compression string) (int64, error) {
	writer, err := NewCompressionWriter(dst, compression)
	if err != nil {
		return 0, err
	}
	defer writer.Close()

	return src.WriteTo(writer)
}

// ReadFrom reads the contents of the src and writes it to the provided target.
// The function automatically detects the compression format from the file extension.
func ReadFrom(src io.Reader, dst *os.File, compression string) (int64, error) {
	reader, err := NewCompressionReader(src, compression)
	if err != nil {
		return 0, err
	}
	defer reader.Close()

	return dst.ReadFrom(reader)
}
