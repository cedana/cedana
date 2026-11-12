package io

import (
	"archive/tar"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
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
func Tar(src string, dst io.Writer, compression string, isFuse bool) (err error) {
	writer, err := NewOptimizedCompressionWriter(dst, compression, isFuse)
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
func Untar(src io.Reader, dest string, compression string) (err error) {
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
			defer outFile.Close()

			if _, err := io.Copy(outFile, tarReader); err != nil {
				return err
			}
		}
	}

	return nil
}

// TarEntry describes a member of a tarball.
type TarEntry struct {
	Name    string
	Size    int64
	ModTime time.Time
	IsDir   bool
}

// TarCompressionFromPath reports whether the path looks like a tarball
// (`x.tar`, `x.tar.lz4`, ...) and, if so, its compression format.
func TarCompressionFromPath(path string) (compression string, ok bool) {
	base := filepath.Base(path)
	if strings.HasSuffix(base, ".tar") {
		return "tar", true
	}
	if !strings.Contains(base, ".tar.") {
		return "", false
	}
	compression, err := CompressionFromExt(base)
	if err != nil {
		return "", false
	}
	return compression, true
}

// ListTar lists the members of the provided tarball.
func ListTar(src io.Reader, compression string) (entries []TarEntry, err error) {
	reader, err := NewCompressionReader(src, compression)
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	tarReader := tar.NewReader(reader)
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			return entries, nil
		}
		if err != nil {
			return nil, err
		}
		entries = append(entries, TarEntry{
			Name:    header.Name,
			Size:    header.Size,
			ModTime: header.ModTime,
			IsDir:   header.Typeflag == tar.TypeDir,
		})
	}
}

// OpenTarEntry positions a reader at the contents of the named member of the
// provided tarball. The returned reader is only valid as long as src is.
func OpenTarEntry(src io.Reader, compression string, name string) (io.Reader, error) {
	reader, err := NewCompressionReader(src, compression)
	if err != nil {
		return nil, err
	}

	tarReader := tar.NewReader(reader)
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			return nil, fmt.Errorf("entry %q not found in tarball", name)
		}
		if err != nil {
			return nil, err
		}
		if header.Name == name && header.Typeflag != tar.TypeDir {
			return tarReader, nil
		}
	}
}

// WriteTo writes the contents from the provided src to the the provided destination.
// Compression format is specified by the compression argument.
func WriteTo(src *os.File, dst io.Writer, compression string) (n int64, err error) {
	writer, err := NewCompressionWriter(dst, compression)
	if err != nil {
		return 0, err
	}
	defer writer.Close()

	return src.WriteTo(writer)
}

// ReadFrom reads the contents of the src and writes it to the provided target.
// The function automatically detects the compression format from the file extension.
func ReadFrom(src io.Reader, dst *os.File, compression string) (n int64, err error) {
	reader, err := NewCompressionReader(src, compression)
	if err != nil {
		return 0, err
	}
	defer reader.Close()

	return dst.ReadFrom(reader)
}
