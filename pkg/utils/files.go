package utils

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

func CopyFile(src, dstFolder string) error {
	sfi, err := os.Stat(src)
	if err != nil {
		return err
	}
	if !sfi.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file", src)
	}

	// Get the base name of the source file
	srcBaseName := filepath.Base(src)

	// Append the source file base name to the destination folder to get the destination file path
	dst := filepath.Join(dstFolder, srcBaseName)

	_, err = os.Stat(dst)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil
		}
	}
	// overwrites file if it already exists in dst
	return copyFileContents(src, dst)
}

// copyFileContents copies the contents of the file named src to the file named
// by dst. The file will be created if it does not already exist. If the
// destination file exists, all it's contents will be replaced by the contents
// of the source file.
func copyFileContents(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer func() {
		cerr := out.Close()
		if err == nil {
			err = cerr
		}
	}()

	if _, err = io.Copy(out, in); err != nil {
		return err
	}
	err = out.Sync()
	return err
}
