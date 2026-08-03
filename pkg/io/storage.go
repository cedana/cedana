package io

import (
	"context"
	"io"
)

type Mode int

const (
	READ_ONLY  Mode = 0
	WRITE_ONLY Mode = 1
)

type Storage interface {
	Open(ctx context.Context, path string) (io.ReadCloser, error)
	Create(ctx context.Context, path string) (io.WriteCloser, error)
	Delete(ctx context.Context, path string) error

	IsDir(ctx context.Context, path string) (bool, error)
	ReadDir(ctx context.Context, path string) ([]string, error)

	IsRemote() bool

	GetPath(ctx context.Context, name string, mode Mode) (path string, cleanup func(), err error)
}
