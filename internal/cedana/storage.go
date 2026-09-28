package cedana

import (
	"fmt"
	"strings"

	"context"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/features"
	cedana_io "github.com/cedana/cedana/pkg/io"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *Server) DeletePath(ctx context.Context, req *daemon.DeletePathReq) (*daemon.DeletePathResp, error) {
  if req.GetPath() == "" {
		return nil, status.Errorf(codes.InvalidArgument, "Path must be provided")
  }
  var storage cedana_io.Storage
  var err error
  checkpointPath := req.GetPath()
  if strings.Contains(checkpointPath, "://") {
    pluginName := fmt.Sprintf("storage/%s", strings.Split(checkpointPath, "://")[0])
    err = features.Storage.IfAvailable(func(name string, newPluginStorage func(context.Context) (cedana_io.Storage, error)) error {
      if newPluginStorage == nil {
        return fmt.Errorf("plugin '%s' does not implement '%s'", name, features.Storage)
      }
      storage, err = newPluginStorage(ctx)
      return err
    }, pluginName)
    if err != nil {
      return nil, status.Error(codes.Unavailable, err.Error())
    }

    err := storage.Delete(ctx, checkpointPath)
    if err != nil {
      return nil, status.Error(codes.Internal, err.Error())
    }
  }
	return nil, status.Errorf(codes.InvalidArgument, "Path does not correspond to any storage plugin")
}
