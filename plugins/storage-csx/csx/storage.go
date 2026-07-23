package csx

import (
	"context"
	"fmt"
	"io"
	"os"
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
  log.Info().Str("storage", "csx").Str("path", path).Msg("Open() called")
  return nil, nil
}

func (s *Storage) Create(ctx context.Context, path string) (io.WriteCloser, error) {
  log.Info().Str("storage", "csx").Str("path", path).Msg("Create() called")
  dumpPath := strings.TrimPrefix(path, PATH_PREFIX)
  size := ctx.Value("STORAGE_SIZE")
  var sizeUint64 uint64
  if size != nil {
    sizeUint64 = uint64(size.(int64))
  }
  req := csx.CreateReq{
    Path: dumpPath,
    Size: sizeUint64,
  }
  resp, err := s.csxClient.Create(ctx, &req)
  if err != nil {
    return nil, err
  }
  file, err := os.Create(resp.GetPath())
  if err != nil {
    return nil, err
  }
  return file, nil
}

func (s *Storage) Delete(ctx context.Context, path string) error {
  log.Info().Str("storage", "csx").Str("path", path).Msg("Delete() called")
  return nil
}

func (s *Storage) IsDir(ctx context.Context, path string) (bool, error) {
  log.Info().Str("storage", "csx").Str("path", path).Msg("IsDir() called")
  return false, nil
}

func (s *Storage) ReadDir(ctx context.Context, path string) ([]string, error) {
  log.Info().Str("storage", "csx").Str("path", path).Msg("ReadDir() called")
  return nil, nil
}

func (s *Storage) IsRemote() bool {
  log.Info().Str("storage", "csx").Msg("IsRemote() called")
  return true
}

func NewStorage(ctx context.Context) (cedana_io.Storage, error) {
  var opts []grpc.DialOption

	const MAX_MSG_SIZE             = 6 << 20 // 6MiB instead of default 4MiB
	opts = append(
		opts,
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(MAX_MSG_SIZE)),
	)
	opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
  conn, err := grpc.NewClient(fmt.Sprintf("unix://%s", cedana_config.Global.CSX.SockPath), opts...)
  if err != nil {
    return nil, err
  }

  csxClient := csxgrpc.NewCSXClient(conn)
  return &Storage{
    ClientConn: conn,
    csxClient: csxClient,
  }, nil
}
