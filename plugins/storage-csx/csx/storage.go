package csx

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/plugins/csx"
	"github.com/rs/zerolog/log"

	cedana_io "github.com/cedana/cedana/pkg/io"
)

const PATH_PREFIX = "csx://"

type Storage struct {
	sockAddr string
}

func (s *Storage) Open(ctx context.Context, path string) (io.ReadCloser, error) {
	conn, csxClient, err := s.newCSXClient()
	if err != nil {
		log.Err(err).Msg("could not establish connection to CSX")
		return nil, err
	}

	objectID := filepath.Base(strings.TrimPrefix(path, PATH_PREFIX))
	resp, err := csxClient.GetPath(ctx, &csx.GetPathReq{
		ID:   objectID,
		Mode: csx.Mode_READ_ONLY,
	})
	if err != nil {
		conn.Close()
		log.Err(err).Msg("could not open file with CSX")
		return nil, err
	}

	log.Debug().Str("path", resp.GetPath()).Str("readID", resp.GetActionID()).Msg("got path from csx")
	return NewFile(ctx, objectID, resp.GetActionID(), resp.GetPath(), conn, csxClient), nil
}

func (s *Storage) Create(ctx context.Context, path string) (io.WriteCloser, error) {
	conn, csxClient, err := s.newCSXClient()
	if err != nil {
		log.Err(err).Msg("could not establish connection to CSX")
		return nil, err
	}

	objectID := filepath.Base(strings.TrimPrefix(path, PATH_PREFIX))
	resp, err := csxClient.GetPath(ctx, &csx.GetPathReq{
		ID:   objectID,
		Mode: csx.Mode_WRITE_ONLY,
	})
	if err != nil {
		conn.Close()
		log.Err(err).Msg("could not create file with CSX")
		return nil, err
	}

	return NewFile(ctx, objectID, resp.GetActionID(), resp.GetPath(), conn, csxClient), nil
}

func (s *Storage) Delete(ctx context.Context, path string) error {
	log.Info().Str("storage", "csx").Str("path", path).Msg("Delete() called")
	return nil
}

func (s *Storage) IsDir(ctx context.Context, path string) (bool, error) {
	return false, nil
}

func (s *Storage) ReadDir(ctx context.Context, path string) ([]string, error) {
	return nil, fmt.Errorf("CSX does not support reading from dir")
}

func (s *Storage) IsRemote() bool {
	return true
}

func NewStorage(ctx context.Context) (cedana_io.Storage, error) {
	return &Storage{
		sockAddr: "/run/csx.sock",
	}, nil
}
