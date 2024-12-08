package filesystem

import (
	"context"
	"os"
	"path/filepath"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/criu"
	"github.com/cedana/cedana/pkg/types"
	runc_keys "github.com/cedana/cedana/plugins/runc/pkg/keys"
	"github.com/cedana/cedana/plugins/runc/pkg/runc"
	"github.com/opencontainers/runc/libcontainer"
	"golang.org/x/sys/unix"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func SetWorkingDirectoryForRestore(next types.Restore) types.Restore {
	return func(ctx context.Context, server types.ServerOpts, nfy *criu.NotifyCallbackMulti, resp *daemon.RestoreResp, req *daemon.RestoreReq) (chan int, error) {
		opts := req.GetDetails().GetRunc()
		workingDir := opts.GetWorkingDir()

		if workingDir != "" {
			oldDir, err := os.Getwd()
			if err != nil {
				return nil, status.Errorf(codes.Internal, "failed to get current working directory: %v", err)
			}
			err = os.Chdir(workingDir)
			if err != nil {
				return nil, status.Errorf(codes.Internal, "failed to set working directory: %v", err)
			}
			defer os.Chdir(oldDir)
		}

		return next(ctx, server, nfy, resp, req)
	}
}

// CRIU has a few requirements for a root directory:
// * it must be a mount point
// * its parent must not be overmounted
// c.config.Rootfs is bind-mounted to a temporary directory
// to satisfy these requirements.
func MountRootDirForRestore(next types.Restore) types.Restore {
	return func(ctx context.Context, server types.ServerOpts, nfy *criu.NotifyCallbackMulti, resp *daemon.RestoreResp, req *daemon.RestoreReq) (exited chan int, err error) {
		container, ok := ctx.Value(runc_keys.CONTAINER_CONTEXT_KEY).(*libcontainer.Container)
		if !ok {
			return nil, status.Errorf(codes.FailedPrecondition, "failed to get container from context")
		}

		criuRoot := filepath.Join(os.TempDir(), "criu-root-"+container.ID())
		if err := os.Mkdir(criuRoot, 0o755); err != nil {
			return nil, status.Errorf(codes.Internal, "failed to create root directory for CRIU: %v", err)
		}
		defer os.RemoveAll(criuRoot)

		criuRoot, err = filepath.EvalSymlinks(criuRoot)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to resolve symlink: %v", err)
		}

		// Mount the rootfs
		err = runc.Mount(container.Config().Rootfs, criuRoot, "", unix.MS_BIND|unix.MS_REC, "")
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to mount rootfs: %v", err)
		}
		defer unix.Unmount(criuRoot, unix.MNT_DETACH)

		return next(ctx, server, nfy, resp, req)
	}
}
