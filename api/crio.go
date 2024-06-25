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

func (s *service) CRIORootfsDump(ctx context.Context, args *task.CRIORootfsDumpArgs) (task *task.CRIORootfsDumpResp, err error) {
	var spec *rspec.Spec

	root := filepath.Join("/var/lib/containers/storage/overlay-containers/", args.ContainerID, "userdata/config.json")

	configFile, err := os.ReadFile(filepath.Join(root))
	if err != nil {
		return task, err
	}
	if err := json.Unmarshal(configFile, &spec); err != nil {
		return task, err
	}

	crio.RootfsCheckpoint(ctx, args.ContainerStorage, args.Dest, args.ContainerID, spec)

	return task, nil
}
