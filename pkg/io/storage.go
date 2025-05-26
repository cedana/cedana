package io

import (
	"io"
)

type Storage interface {
	Open(path string) (io.ReadCloser, error)
	Create(path string) (io.WriteCloser, error)
	Delete(path string) error
	ReadDir(path string) ([]string, error)

	IsRemote() bool
}
