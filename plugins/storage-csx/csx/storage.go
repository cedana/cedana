package csx

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"buf.build/gen/go/cedana/cedana/grpc/go/plugins/csx/csxgrpc"
	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/plugins/csx"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	cedana_io "github.com/cedana/cedana/pkg/io"
)

const PATH_PREFIX = "csx://"

type Storage struct {
	csxClient csxgrpc.CSXClient
	*grpc.ClientConn
}

func (s *Storage) Open(ctx context.Context, path string) (io.ReadCloser, error) {
	objectID := filepath.Base(strings.TrimPrefix(path, PATH_PREFIX))
	resp, err := s.csxClient.GetPath(ctx, &csx.GetPathReq{
		ID:   objectID,
		Mode: csx.Mode_READ_ONLY,
	})
	if err != nil {
		s.ClientConn.Close()
		log.Err(err).Msg("could not open file with CSX")
		return nil, err
	}
	log.Debug().Str("path", resp.GetPath()).Str("readID", resp.GetActionID()).Msg("got path from csx")
	return NewReader(ctx, resp.GetPath(), objectID, resp.GetActionID(), s.csxClient, s.ClientConn)
}

func (s *Storage) Create(ctx context.Context, path string) (io.WriteCloser, error) {
	objectID := filepath.Base(strings.TrimPrefix(path, PATH_PREFIX))
	resp, err := s.csxClient.GetPath(ctx, &csx.GetPathReq{
		ID:   objectID,
		Mode: csx.Mode_WRITE_ONLY,
	})
	if err != nil {
		s.ClientConn.Close()
		log.Err(err).Msg("could not create file with CSX")
		return nil, err
	}

	return NewWriter(ctx, resp.GetPath(), objectID, resp.GetActionID(), s.csxClient, s.ClientConn)
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
	return false
}

func (s *Storage) CreatePath(ctx context.Context, dir, name string) (path string, cleanup func() error, err error) {
	// Strip out csx://
	if dir != PATH_PREFIX {
		return "", nil, fmt.Errorf("invalid dir for CSX")
	}
	objectID := filepath.Base(name)
	resp, err := s.csxClient.GetPath(ctx, &csx.GetPathReq{
		ID:   objectID,
		Mode: csx.Mode_WRITE_ONLY,
	})
	if err != nil {
		s.ClientConn.Close()
		log.Err(err).Str("name", name).Str("objectID", objectID).Msg("could not get path from CSX")
		return "", nil, err
	}

	cleanup = func() error {
		_, closePathErr := s.csxClient.ClosePath(ctx, &csx.ClosePathReq{
			ActionID: resp.GetActionID(),
			ID:       objectID,
		})
		return errors.Join(closePathErr, s.ClientConn.Close())
	}
	log.Debug().Str("Path", resp.GetPath()).Msg("Got Path from CSX")
	return resp.GetPath(), cleanup, nil
}

func (s *Storage) ReadPath(ctx context.Context, path string) (string, func() error, error) {
	if !strings.HasPrefix(path, PATH_PREFIX) {
		return "", nil, fmt.Errorf("Invalid Prefix for CSX")
	}

	path = filepath.Base(strings.TrimPrefix(path, PATH_PREFIX))
	resp, err := s.csxClient.GetPath(ctx, &csx.GetPathReq{
		ID:   path,
		Mode: csx.Mode_READ_ONLY,
	})
	if err != nil {
		s.ClientConn.Close()
		log.Err(err).Str("objectID", path).Msg("could not get path from CSX")
		return "", nil, err
	}
	cleanup := func() error {
		_, closePathErr := s.csxClient.ClosePath(ctx, &csx.ClosePathReq{
			ActionID: resp.GetActionID(),
			ID:       path,
		})
		return errors.Join(closePathErr, s.ClientConn.Close())
	}
	return resp.GetPath(), cleanup, nil
}

func NewStorage(ctx context.Context) (cedana_io.Storage, error) {
	var opts []grpc.DialOption

	opts = append(
		opts,
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(4<<20)),
	)
	opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	conn, err := grpc.NewClient(fmt.Sprintf("unix://%s", "/run/csx.sock"), opts...)
	if err != nil {
		log.Err(err).Msg("could not establish connection to CSX")
		return nil, err
	}

	csxClient := csxgrpc.NewCSXClient(conn)
	return &Storage{
		ClientConn: conn,
		csxClient:  csxClient,
	}, nil
}
