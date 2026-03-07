package propagator

import (
	"context"
	"fmt"
	"io"
	"strings"

	sdk "github.com/cedana/cedana-go-sdk"
	"github.com/cedana/cedana-go-sdk/models"
	"github.com/cedana/cedana-go-sdk/v2"
	"github.com/cedana/cedana/pkg/config"
	cedana_io "github.com/cedana/cedana/pkg/io"
)

const PATH_PREFIX = "cedana://"

// Cedana managed storage
type Storage struct {
	*v2.V2RequestBuilder
}

func NewStorage(ctx context.Context) (cedana_io.Storage, error) {
	url := config.Global.Connection.URL
	authToken := config.Global.Connection.AuthToken

	// Creating the client is no extra compute/work as this is not a durable connection
	return &Storage{sdk.NewCedanaClient(url, authToken).V2()}, nil
}

func (s *Storage) Open(ctx context.Context, path string) (io.ReadCloser, error) {
	path, err := s.sanitizePath(path)
	if err != nil {
		return nil, err
	}

	downloadUrl, err := s.Files().ByPath(path).Get(ctx, nil)
	if err != nil {
		switch e := err.(type) {
		case *models.ApiError:
			return nil, fmt.Errorf("%d: %s", e.ResponseStatusCode, e.Message)
		default:
			return nil, err
		}
	}

	return NewDownloadableFile(ctx, *downloadUrl), nil
}

func (s *Storage) Create(ctx context.Context, path string) (io.WriteCloser, error) {
	path, err := s.sanitizePath(path)
	if err != nil {
		return nil, err
	}

	uploadUrl, err := s.Files().ByPath(path).Put(ctx, nil)
	if err != nil {
		switch e := err.(type) {
		case *models.ApiError:
			return nil, fmt.Errorf("%d: %s", e.ResponseStatusCode, e.Message)
		default:
			return nil, err
		}
	}

	return NewUploadableFile(ctx, *uploadUrl), nil
}

func (s *Storage) Delete(_ context.Context, path string) error {
	path, err := s.sanitizePath(path)
	if err != nil {
		return err
	}

	return fmt.Errorf("this operation is currently not supported for cedana storage")
}

func (s *Storage) IsDir(_ context.Context, path string) (bool, error) {
	path, err := s.sanitizePath(path)
	if err != nil {
		return false, err
	}

	return true, nil // Cedana storage does not differentiate between files and directories
}

func (s *Storage) ReadDir(ctx context.Context, path string) ([]string, error) {
	path, err := s.sanitizePath(path)
	if err != nil {
		return nil, err
	}

	list, err := s.Files().Dir().ByPath(path).Get(ctx, nil)
	if err != nil {
		switch e := err.(type) {
		case *models.ApiError:
			return nil, fmt.Errorf("%d: %s", e.ResponseStatusCode, e.Message)
		default:
			return nil, err
		}
	}
	return list, nil
}

func (s *Storage) IsRemote() bool {
	return true
}

/////////////
// Helpers //
/////////////

func (s *Storage) sanitizePath(path string) (string, error) {
	if !strings.HasPrefix(path, PATH_PREFIX) {
		return "", fmt.Errorf("path must start with %s", PATH_PREFIX)
	}

	path = strings.TrimPrefix(path, PATH_PREFIX)
	path = strings.TrimPrefix(path, "/")

	if path == "" {
		return "", fmt.Errorf("path cannot be empty")
	}

	return path, nil
}
