package utils

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func ZipFolder(folderPath, zipFilePath string) error {
	cmd := exec.Command("zip", "-r", zipFilePath, folderPath)
	err := cmd.Run()
	if err != nil {
		return err
	}
	return nil
}

func UnzipFolder(zipPath, destPath string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer r.Close()

	rootDir := strings.SplitN(r.File[0].Name, "/", 2)[0]

	for _, f := range r.File {
		rc, err := f.Open()
		if err != nil {
			return err
		}

		defer rc.Close()

		// Skip the root directory in the zip file
		if !strings.HasPrefix(f.Name, rootDir+"/") {
			continue
		}

		// Construct the path for the file to be extracted
		relativePath := strings.TrimPrefix(f.Name, rootDir+"/")
		targetPath := filepath.Join(destPath, relativePath)

		// Create all necessary directories
		if f.FileInfo().IsDir() {
			// Make Folder
			fmt.Printf("Creating Folder: %s\n", targetPath)
			err := os.MkdirAll(targetPath, f.Mode())
			if err != nil {
				return err
			}
		} else {
			err := os.MkdirAll(filepath.Dir(targetPath), os.ModePerm)
			if err != nil {
				return err
			}
			fmt.Printf("Creating File: %s\n", targetPath)
			outFile, err := os.OpenFile(targetPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
			if err != nil {
				return err
			}

			_, err = io.Copy(outFile, rc)
			if err != nil {
				return err
			}

			outFile.Close()
		}
	}
	return nil

}
