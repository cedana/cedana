package api

import (
	"context"
	"path/filepath"

	"github.com/cedana/cedana/api/crio"
	"github.com/cedana/cedana/api/runc"
	"github.com/cedana/cedana/api/services/task"
)

func (s *service) CRIORootfsDump(ctx context.Context, args *task.CRIORootfsDumpArgs) (task *task.CRIORootfsDumpResp, err error) {

	spec, err := runc.GetSpecById(filepath.Join(args.ContainerStorage, "userdata"), args.ContainerID)
	if err != nil {
		return task, err
	}

	crio.RootfsCheckpoint(ctx, args.ContainerStorage, args.Dest, args.ContainerID, spec)

	return task, nil
}
