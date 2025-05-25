package propagator

import (
	"context"
	"fmt"
	"io"
	"strings"

	sdk "github.com/cedana/cedana-go-sdk"
	"github.com/cedana/cedana-go-sdk/v2"
	"github.com/cedana/cedana/pkg/config"
	cedana_io "github.com/cedana/cedana/pkg/io"
)

const PATH_PREFIX = "cedana://"

// Cedana managed storage
type Storage struct {
	ctx context.Context
	*v2.V2RequestBuilder
}

func NewStorage(ctx context.Context) (cedana_io.Storage, error) {
	url := config.Global.Connection.URL
	authToken := config.Global.Connection.AuthToken

	// Creating the client is no extra compute/work as this is not a durable connection
	return &Storage{ctx, sdk.NewCedanaClient(url, authToken).V2()}, nil
}

func (s *Storage) Open(path string) (io.ReadCloser, error) {
	if !strings.HasPrefix(path, PATH_PREFIX) {
		return nil, fmt.Errorf("path must start with %s", PATH_PREFIX)
	}

	path = strings.TrimPrefix(path, PATH_PREFIX)
	id := strings.Split(path, "/")[0] // FIXME: actual path of file ignore for now

	downloadUrl, err := s.Checkpoints().Download().ById(id).Get(s.ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get download URL: %w", err)
	}

	return NewCheckpoint(id, *downloadUrl, ""), nil
}

func (s *Storage) Create(path string) (io.ReadWriteCloser, error) {
	if !strings.HasPrefix(path, PATH_PREFIX) {
		return nil, fmt.Errorf("path must start with %s", PATH_PREFIX)
	}

	_ = strings.TrimPrefix(path, PATH_PREFIX) // FIXME: id from path ignored for now

	id, err := s.Checkpoints().Post(s.ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create checkpoint: %w", err)
	}

	uploadUrl, err := s.Checkpoints().Upload().ById(*id).Patch(s.ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get upload URL: %w", err)
	}

	return NewCheckpoint(*id, "", *uploadUrl), nil
}

func (s *Storage) Delete(path string) error {
	return fmt.Errorf("this operation is currently not supported for cedana storage")
}

func (s *Storage) ReadDir(path string) ([]string, error) {
	return nil, fmt.Errorf("this operation is currently not supported for cedana storage")
}

func (s *Storage) IsRemote() bool {
	return true
}
