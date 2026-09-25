package cedana

import (
	"context"
	"fmt"
	"strings"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/features"
	cedana_io "github.com/cedana/cedana/pkg/io"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *Server) DeleteCheckpoint(ctx context.Context, req *daemon.DeleteCheckpointReq) (*daemon.DeleteCheckpointResp, error) {
	if req.ID == "" && !req.HasPath() {
		return nil, status.Errorf(codes.InvalidArgument, "ID must be provided")
	}

	log.Debug().Str("ID", req.GetID()).Str("path", req.GetPath()).Msg("got delete request")
	if req.HasPath() {
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
	} else {
		checkpoint := s.jobs.GetCheckpoint(req.GetID())
		if checkpoint == nil {
			return nil, status.Errorf(codes.NotFound, "checkpoint not found")
		}
		s.jobs.DeleteCheckpoint(req.GetID())
	}

	return &daemon.DeleteCheckpointResp{}, nil
}
