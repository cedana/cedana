package clh

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// copyFiltered copies only directories and persist.json files while preserving the directory structure.
func copyFiltered(src string, destRoot string, baseSrc string) error {
	err := filepath.WalkDir(src, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Get relative path to maintain the hierarchy
		relPath, err := filepath.Rel(baseSrc, path)
		if err != nil {
			return err
		}

		destPath := filepath.Join(destRoot, relPath)

		if entry.IsDir() {
			// Always create directories in the destination
			return os.MkdirAll(destPath, os.ModePerm)
		} else if entry.Name() == "persist.json" {
			// Only copy "persist.json" files
			return copyFile(path, destPath)
		}
		return nil
	})

	return err
}

func copyFile(src string, dest string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("failed to open source file: %w", err)
	}
	defer srcFile.Close()

	destFile, err := os.Create(dest)
	if err != nil {
		return fmt.Errorf("failed to create destination file: %w", err)
	}
	defer destFile.Close()

	_, err = io.Copy(destFile, srcFile)
	if err != nil {
		return fmt.Errorf("failed to copy file: %w", err)
	}

	// Preserve file permissions
	srcInfo, err := srcFile.Stat()
	if err != nil {
		return fmt.Errorf("failed to get source file info: %w", err)
	}
	return os.Chmod(dest, srcInfo.Mode())
}
