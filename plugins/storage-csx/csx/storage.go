package csx

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"buf.build/gen/go/cedana/cedana/grpc/go/plugins/csx/csxgrpc"
	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/plugins/csx"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	cedana_config "github.com/cedana/cedana/pkg/config"
	cedana_io "github.com/cedana/cedana/pkg/io"
)

const PATH_PREFIX = "csx://"

type Storage struct {
  csxClient csxgrpc.CSXClient
  *grpc.ClientConn
}

func (s *Storage) Open(ctx context.Context, path string) (io.ReadCloser, error) {
  objectID := filepath.Base(strings.TrimPrefix(path, PATH_PREFIX))
  resp, err := s.csxClient.Open(ctx, &csx.OpenReq{ObjectID: objectID})
  if err != nil {
    log.Err(err).Msg("could not open file with CSX")
    return nil, err
  }
  return NewReader(ctx, resp.GetPath(), objectID, resp.GetReadID(), s.csxClient)
}

func (s *Storage) Create(ctx context.Context, path string) (io.WriteCloser, error) {
  objectID := filepath.Base(strings.TrimPrefix(path, PATH_PREFIX))
  resp, err := s.csxClient.Create(ctx, &csx.CreateReq{ObjectID: objectID})
  if err != nil {
    log.Err(err).Msg("could not create file with CSX")
    return nil, err
  }

  return NewWriter(ctx, resp.GetPath(), objectID, resp.GetCreateID(), s.csxClient)
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
  var opts []grpc.DialOption

	opts = append(
		opts,
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(4 << 20)),
	)
	opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
  conn, err := grpc.NewClient(fmt.Sprintf("unix://%s", cedana_config.Global.CSX.SockPath), opts...)
  if err != nil {
    log.Err(err).Msg("could not establish connection to CSX")
    return nil, err
  }

  csxClient := csxgrpc.NewCSXClient(conn)
  return &Storage{
    ClientConn: conn,
    csxClient: csxClient,
  }, nil
}
