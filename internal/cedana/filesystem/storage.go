package filesystem

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Default filesystem storage
type Storage struct{}

func (s *Storage) Open(_ context.Context, path string) (io.ReadCloser, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	return file, nil
}

func (s *Storage) Create(_ context.Context, path string) (io.WriteCloser, error) {
	file, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("failed to create file: %w", err)
	}
	return file, nil
}

func (s *Storage) Delete(_ context.Context, path string) error {
	err := os.Remove(path)
	if err != nil {
		return fmt.Errorf("failed to delete file: %w", err)
	}
	return nil
}

func (s *Storage) IsDir(_ context.Context, path string) (bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		return false, fmt.Errorf("failed to stat path: %w", err)
	}
	return info.IsDir(), nil
}

func (s *Storage) ReadDir(_ context.Context, path string) ([]string, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read directory: %w", err)
	}

	var files []string
	for _, entry := range entries {
		files = append(files, entry.Name())
	}
	return files, nil
}

func (s *Storage) IsRemote() bool {
	return false
}

func (s *Storage) ReadPath(_ context.Context, path string) (string, func() error, error) {
	return path, nil, nil
}

func (s *Storage) CreatePath(_ context.Context, dir, name string) (path string, cleanup func() error, err error) {
	// Check if the provided dir exists
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return "", nil, fmt.Errorf("dump dir does not exist: %s", dir)
	}

	return filepath.Join(dir, name), nil, nil
}
