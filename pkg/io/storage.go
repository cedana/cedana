package io

import (
	"io"
)

type (
	Storage interface {
		Open(path string) (io.ReadCloser, error)
		Create(path string) (io.ReadWriteCloser, error)
		Delete(path string) error

		IsRemote() bool
	}
)
