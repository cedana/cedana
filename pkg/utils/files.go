package utils

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// CreateTarball creates a tarball from the provided sources and writes it to the destination.
// The desination should be a path without any file extension, as the function will add extension
// based on the compression format specified.
// XXX: Works only with files, not directories.
func Tar(src string, tarball string, compression string) (string, error) {
	switch compression {
	case "gzip":
		tarball += ".tar.gz"
	case "tar":
		tarball += ".tar"
	case "none":
		tarball += ".tar"
	case "":
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
	case "gzip":
		writer = gzip.NewWriter(file)
		defer writer.Close()
	case "tar":
		writer = file
	case "none":
		writer = file
	case "":
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

		// Construct the full path for the file
		target := filepath.Join(dest, header.Name)

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
