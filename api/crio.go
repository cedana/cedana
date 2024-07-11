package api

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/cedana/cedana/api/crio"
	"github.com/cedana/cedana/api/services/task"
	rspec "github.com/opencontainers/runtime-spec/specs-go"
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

	if err := crio.CRIORootfsMerge(args.OriginalImageRef, args.NewImageRef, args.RootfsDiffPath, args.ContainerStorage); err != nil {
		return resp, err
	}

	if err := crio.CRIOImagePush(args.NewImageRef); err != nil {
		return resp, err
	}

	resp.Message = "success"

	return resp, nil
}
