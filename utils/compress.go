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
	// TODO BS zip needs to be installed on the system
	// Add err handling for zip not being installed
	cmd := exec.Command("zip", "-r", zipFilePath, folderPath)

	err := cmd.Run()
	if err != nil {
		return err
	}

	return nil
}

func UnzipFolder(src, dest string) error {
	r, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer r.Close()

	for _, f := range r.File {
		// Ignore directories, only process files
		if !f.FileInfo().IsDir() {
			fpath := filepath.Join(dest, filepath.Base(f.Name))
			if !strings.HasPrefix(fpath, filepath.Clean(dest)+string(os.PathSeparator)) {
				return fmt.Errorf("%s: illegal file path", fpath)
			}

			if err = os.MkdirAll(filepath.Dir(fpath), os.ModePerm); err != nil {
				return err
			}

			outFile, err := os.OpenFile(fpath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
			if err != nil {
				return err
			}

			rc, err := f.Open()
			if err != nil {
				return err
			}

			_, err = io.Copy(outFile, rc)
			outFile.Close()
			rc.Close()

			if err != nil {
				return err
			}
		}
	}
	return nil
}
