package io

import (
	"context"
	"io"
)

type Storage interface {
	Open(ctx context.Context, path string) (io.ReadCloser, error)
	Create(ctx context.Context, path string) (io.WriteCloser, error)
	Delete(ctx context.Context, path string) error

	IsDir(ctx context.Context, path string) (bool, error)
	ReadDir(ctx context.Context, path string) ([]string, error)

	IsRemote() bool
}
