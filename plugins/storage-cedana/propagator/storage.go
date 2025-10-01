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
	path, err := s.sanitizePath(path)
	if err != nil {
		return nil, err
	}

	downloadUrl, err := s.Files().ByPath(path).Get(s.ctx, nil)
	if err != nil {
		switch v := err.(type) {
		case *models.HttpError:
			return nil, fmt.Errorf("failed to get download URL: %s", *v.GetMessage())
		default:
			return nil, fmt.Errorf("failed to get download URL: %v", err)
		}
	}

	return NewDownloadableFile(s.ctx, *downloadUrl), nil
}

func (s *Storage) Create(path string) (io.WriteCloser, error) {
	path, err := s.sanitizePath(path)
	if err != nil {
		return nil, err
	}

	uploadUrl, err := s.Files().ByPath(path).Put(s.ctx, nil)
	if err != nil {
		switch v := err.(type) {
		case *models.HttpError:
			return nil, fmt.Errorf("failed to get upload URL: %s", *v.GetMessage())
		default:
			return nil, fmt.Errorf("failed to get upload URL: %v", err)
		}
	}

	return NewUploadableFile(s.ctx, *uploadUrl), nil
}

func (s *Storage) Delete(path string) error {
	path, err := s.sanitizePath(path)
	if err != nil {
		return err
	}

	return fmt.Errorf("this operation is currently not supported for cedana storage")
}

func (s *Storage) IsDir(path string) (bool, error) {
	path, err := s.sanitizePath(path)
	if err != nil {
		return false, err
	}

	return true, nil // Cedana storage does not differentiate between files and directories
}

func (s *Storage) ReadDir(path string) ([]string, error) {
	path, err := s.sanitizePath(path)
	if err != nil {
		return nil, err
	}

	list, err := s.Files().Dir().ByPath(path).Get(s.ctx, nil)
	if err != nil {
		switch v := err.(type) {
		case *models.HttpError:
			return nil, fmt.Errorf("failed to list directory: %s", *v.GetMessage())
		default:
			return nil, fmt.Errorf("failed to list directory: %v", err)
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
