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
// based on the compression format used.
func CreateTarball(sources []string, name string, compression string) (string, error) {
	switch compression {
	case "gzip":
		name += ".tar.gz"
	case "tar":
		name += ".tar"
	case "none":
		name += ".tar"
	case "":
		name += ".tar"
		break
	default:
		return "", fmt.Errorf("Unsupported compression format: %s", compression)
	}

	file, err := os.Create(name)
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
		os.Remove(name)
		return "", fmt.Errorf("Unsupported compression format: %s", compression)
	}

	tarWriter := tar.NewWriter(writer)
	defer tarWriter.Close()

	for _, file := range sources {
		err := addFileToTarWriter(file, tarWriter)
		if err != nil {
			os.Remove(name)
			return "", err
		}
	}

	return name, nil
}

//////////////////////////
//// Helper Functions ////
//////////////////////////

// Private methods

func addFileToTarWriter(filePath string, tarWriter *tar.Writer) error {
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("Could not open file '%s' for reading, got error '%s'", filePath, err)
	}
	defer file.Close()

	stat, err := file.Stat()
	if err != nil {
		return fmt.Errorf("Could not get file info for file '%s', got error '%s'", filePath, err)
	}

	header := &tar.Header{
		Name:    filePath,
		Size:    stat.Size(),
		Mode:    int64(stat.Mode()),
		ModTime: stat.ModTime(),
	}

	err = tarWriter.WriteHeader(header)
	if err != nil {
		return fmt.Errorf("Could not write header for file '%s' to tarball, got error '%s'", filePath, err)
	}

	_, err = io.Copy(tarWriter, file)
	if err != nil {
		return fmt.Errorf("Could not copy file '%s' to tarball, got error '%s'", filePath, err)
	}

	return nil
}

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
