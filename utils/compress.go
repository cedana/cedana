package utils

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
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

func TarFolder(srcFolder, destTar string) error {
	file, err := os.Create(destTar)
	if err != nil {
		return err
	}
	defer file.Close()

	gw := gzip.NewWriter(file)
	defer gw.Close()

	tw := tar.NewWriter(gw)
	defer tw.Close()

	err = filepath.Walk(srcFolder, func(file string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		header, err := tar.FileInfoHeader(fi, file)
		if err != nil {
			return err
		}

		// Adjust the file's path to exclude the base directory
		relPath, err := filepath.Rel(srcFolder, file)
		if err != nil {
			return err
		}
		header.Name = relPath

		if err := tw.WriteHeader(header); err != nil {
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

		_, err = io.Copy(tw, srcFile)
		return err
	})

	return err
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

func UntarFolder(srcTarGz, destFolder string) error {
	// Open the gzip file
	file, err := os.Open(srcTarGz)
	if err != nil {
		return err
	}
	defer file.Close()

	// Create a gzip reader on top of the file reader
	gr, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer gr.Close()

	// Create a tar reader on top of the gzip reader
	tr := tar.NewReader(gr)

	// Iterate through the files in the tarball
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break // End of tarball
		}
		if err != nil {
			return err
		}

		// Construct the full path for the file
		target := filepath.Join(destFolder, header.Name)

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

			if _, err := io.Copy(outFile, tr); err != nil {
				outFile.Close()
				return err
			}
			outFile.Close()
		}
	}

	return nil
}
