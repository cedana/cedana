package api

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/cedana/cedana/pkg/api/crio"
	"github.com/cedana/cedana/pkg/api/services/task"
	rspec "github.com/opencontainers/runtime-spec/specs-go"
	"github.com/rs/zerolog/log"
)

func (s *service) CRIORootfsDump(ctx context.Context, args *task.CRIORootfsDumpArgs) (resp *task.CRIORootfsDumpResp, err error) {
	var spec *rspec.Spec
	resp = &task.CRIORootfsDumpResp{}
	root := filepath.Join("/var/lib/containers/storage/overlay-containers/", args.ContainerID, "userdata/config.json")

	configFile, err := os.ReadFile(filepath.Join(root))
	if err != nil {
		return resp, err
	}
	if err := json.Unmarshal(configFile, &spec); err != nil {
		return resp, err
	}

	diffPath, err := crio.RootfsCheckpoint(ctx, args.ContainerStorage, args.Dest, args.ContainerID, spec)
	if err != nil {
		return resp, err
	}

	resp.Dest = diffPath

	return resp, nil
}

func (s *service) CRIOImagePush(ctx context.Context, args *task.CRIOImagePushArgs) (resp *task.CRIOImagePushResp, err error) {
	resp = &task.CRIOImagePushResp{}

	log.Debug().Msgf("CRIO image merge started with original image: %s and new image: %s", args.OriginalImageRef, args.NewImageRef)
	if err := crio.RootfsMerge(ctx, args.OriginalImageRef, args.NewImageRef, args.RootfsDiffPath, args.ContainerStorage, args.RegistryAuthTokenPull); err != nil {
		return resp, err
	}

	log.Debug().Msgf("CRIO image push started with new image: %s", args.NewImageRef)
	if err := crio.ImagePush(ctx, args.NewImageRef, args.RegistryAuthTokenPush); err != nil {
		return resp, err
	}

	resp.Message = "success"

	return resp, nil
}
