package utils

import (
	"archive/zip"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func CompressFolder(folderPath, zipFilePath string) error {
	zipFile, err := os.Create(zipFilePath)
	if err != nil {
		return err
	}
	defer zipFile.Close()

	archive := zip.NewWriter(zipFile)
	defer archive.Close()

	filepath.Walk(folderPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}

		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()

		header, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}

		header.Name = path
		header.Method = zip.Deflate

		// (see https://golang.org/src/archive/tar/common.go?#L626)
		// these need to be relative paths
		header.Name = filepath.Join(filepath.Base(folderPath), filepath.Base(path))

		writer, err := archive.CreateHeader(header)
		if err != nil {
			return err
		}

		_, err = io.Copy(writer, file)
		return err
	})

	return nil
}

// The decompress function is broken - this is a hack for now
func UnzipFolder(zipPath, destPath string) error {
	dirName, err := getDirectoryName(zipPath)
	if err != nil {
		return err
	}

	d := *dirName

	cmd := exec.Command("unzip", "-j", zipPath, d+"/*", "-d", destPath)
	err = cmd.Run()
	if err != nil {
		return err
	}
	return nil
}

func getDirectoryName(zipFilePath string) (*string, error) {
	var dirName string
	r, err := zip.OpenReader(zipFilePath)
	if err != nil {
		return nil, err
	}
	defer r.Close()

	// just grab the first directorDecompressZipToTempFolde
	// get first part of name (the folder)
	f := r.File[0]
	dirName = strings.Split(f.Name, "/")[0]
	return &dirName, nil
}

func DecompressFolder(zipFilePath, destination string) error {
	zipReader, err := zip.OpenReader(zipFilePath)
	if err != nil {
		return err
	}
	defer zipReader.Close()

	for _, file := range zipReader.File {
		path := filepath.Join(destination, file.Name)
		if file.FileInfo().IsDir() {
			os.MkdirAll(path, file.Mode())
			continue
		}

		fileReader, err := file.Open()
		if err != nil {
			return err
		}
		defer fileReader.Close()

		targetFile, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, file.Mode())
		if err != nil {
			return err
		}
		defer targetFile.Close()

		if _, err := io.Copy(targetFile, fileReader); err != nil {
			return err
		}
	}

	return nil
}
