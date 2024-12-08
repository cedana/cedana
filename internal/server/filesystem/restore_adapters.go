package filesystem

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	criu_proto "buf.build/gen/go/cedana/criu/protocolbuffers/go/criu"
	"github.com/cedana/cedana/pkg/criu"
	"github.com/cedana/cedana/pkg/types"
	"github.com/cedana/cedana/pkg/utils"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// This adapter decompresses (if required) the dump to a temporary directory for restore.
// Automatically detects the compression format from the file extension.
func PrepareRestoreDir(next types.Restore) types.Restore {
	return func(ctx context.Context, server types.ServerOpts, nfy *criu.NotifyCallbackMulti, resp *daemon.RestoreResp, req *daemon.RestoreReq) (chan int, error) {
		path := req.GetPath()
		stat, err := os.Stat(path)
		if err != nil {
			return nil, status.Errorf(codes.NotFound, "path error: %s", path)
		}

		var dir *os.File
		var imagesDirectory string

		if stat.IsDir() {
			imagesDirectory = path
		} else {
			// Create a temporary directory for the restore
			imagesDirectory = filepath.Join(os.TempDir(), fmt.Sprintf("restore-%d", time.Now().Unix()))
			if err := os.Mkdir(imagesDirectory, RESTORE_DIR_PERMS); err != nil {
				return nil, status.Errorf(codes.Internal, "failed to create restore dir: %v", err)
			}
			defer os.RemoveAll(imagesDirectory)

			log.Debug().Str("path", path).Str("dir", imagesDirectory).Msg("decompressing dump")

			// Decompress the dump
			if err := utils.Untar(path, imagesDirectory); err != nil {
				return nil, status.Errorf(codes.Internal, "failed to decompress dump: %v", err)
			}
		}

		dir, err = os.Open(imagesDirectory)
		if err != nil {
			os.RemoveAll(imagesDirectory)
			return nil, status.Errorf(codes.Internal, "failed to open dump dir: %v", err)
		}
		defer dir.Close()

		if req.GetCriu() == nil {
			req.Criu = &criu_proto.CriuOpts{}
		}

		req.GetCriu().ImagesDir = proto.String(imagesDirectory)
		req.GetCriu().ImagesDirFd = proto.Int32(int32(dir.Fd()))

		return next(ctx, server, nfy, resp, req)
	}
}
