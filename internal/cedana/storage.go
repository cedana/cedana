package cedana

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"buf.build/gen/go/cedana/cedana/grpc/go/daemon/daemongrpc"
	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/internal/cedana/filesystem"
	"github.com/cedana/cedana/pkg/features"
	cedana_io "github.com/cedana/cedana/pkg/io"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const READ_PATH_CHUNK_SIZE = 1 << 20 // 1 MiB

func (s *Server) DeletePath(ctx context.Context, req *daemon.DeletePathReq) (*daemon.DeletePathResp, error) {
	checkpointPath := req.GetPath()
	if checkpointPath == "" {
		return nil, status.Errorf(codes.InvalidArgument, "Path must be provided")
	}
	if !strings.Contains(checkpointPath, "://") {
		return nil, status.Errorf(codes.InvalidArgument, "Path does not correspond to any storage plugin")
	}
	storage, err := storageForPath(ctx, checkpointPath)
	if err != nil {
		return nil, status.Error(codes.Unavailable, err.Error())
	}

	err = storage.Delete(ctx, checkpointPath)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &daemon.DeletePathResp{}, nil
}

// ListPath lists the entries of a path. For a checkpoint tarball these are the
// archive's members, for a directory its children, otherwise the file itself.
func (s *Server) ListPath(ctx context.Context, req *daemon.ListPathReq) (*daemon.ListPathResp, error) {
	path := req.GetPath()
	if path == "" {
		return nil, status.Errorf(codes.InvalidArgument, "Path must be provided")
	}
	storage, err := storageForPath(ctx, path)
	if err != nil {
		return nil, status.Error(codes.Unavailable, err.Error())
	}

	resp := &daemon.ListPathResp{}

	if compression, ok := cedana_io.TarCompressionFromPath(path); ok {
		file, err := storage.Open(ctx, path)
		if err != nil {
			return nil, status.Errorf(codes.NotFound, "failed to open %s: %v", path, err)
		}
		defer file.Close()

		entries, err := cedana_io.ListTar(file, compression)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to list %s: %v", path, err)
		}
		for _, entry := range entries {
			resp.Entries = append(resp.Entries, &daemon.PathEntry{
				Name:    entry.Name,
				Size:    entry.Size,
				ModTime: entry.ModTime.UnixMilli(),
				IsDir:   entry.IsDir,
			})
		}
		return resp, nil
	}

	isDir, err := storage.IsDir(ctx, path)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "failed to stat %s: %v", path, err)
	}
	if !isDir {
		resp.Entries = append(resp.Entries, &daemon.PathEntry{Name: filepath.Base(path)})
		return resp, nil
	}

	names, err := storage.ReadDir(ctx, path)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to read %s: %v", path, err)
	}
	_, local := storage.(*filesystem.Storage)
	for _, name := range names {
		entry := &daemon.PathEntry{Name: filepath.Base(name)}
		if local {
			// Only the local filesystem gives us sizes cheaply
			if info, err := os.Stat(filepath.Join(path, name)); err == nil {
				entry.Size = info.Size()
				entry.ModTime = info.ModTime().UnixMilli()
				entry.IsDir = info.IsDir()
			}
		}
		resp.Entries = append(resp.Entries, entry)
	}
	return resp, nil
}

// ReadPath streams the contents of a path, or of an entry inside it (an archive
// member for a checkpoint tarball, a child for a directory).
func (s *Server) ReadPath(req *daemon.ReadPathReq, stream daemongrpc.Daemon_ReadPathServer) error {
	ctx := stream.Context()
	path := req.GetPath()
	entry := req.GetEntry()
	if path == "" {
		return status.Errorf(codes.InvalidArgument, "Path must be provided")
	}
	if strings.Contains(entry, "..") || filepath.IsAbs(entry) {
		return status.Errorf(codes.InvalidArgument, "Entry must be relative to the path")
	}
	storage, err := storageForPath(ctx, path)
	if err != nil {
		return status.Error(codes.Unavailable, err.Error())
	}

	var reader io.Reader
	compression, isTar := cedana_io.TarCompressionFromPath(path)
	if isTar && entry != "" {
		file, err := storage.Open(ctx, path)
		if err != nil {
			return status.Errorf(codes.NotFound, "failed to open %s: %v", path, err)
		}
		defer file.Close()
		reader, err = cedana_io.OpenTarEntry(file, compression, entry)
		if err != nil {
			return status.Errorf(codes.NotFound, "failed to open %s in %s: %v", entry, path, err)
		}
	} else {
		if entry != "" {
			path = path + "/" + entry // do not use filepath.Join as it removes a slash (for remote)
		}
		file, err := storage.Open(ctx, path)
		if err != nil {
			return status.Errorf(codes.NotFound, "failed to open %s: %v", path, err)
		}
		defer file.Close()
		reader = file
	}

	buf := make([]byte, READ_PATH_CHUNK_SIZE)
	for {
		n, err := reader.Read(buf)
		if n > 0 {
			if err := stream.Send(&daemon.ReadPathResp{Data: buf[:n]}); err != nil {
				return err
			}
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return status.Errorf(codes.Internal, "failed to read %s: %v", path, err)
		}
	}
}

// storageForPath returns the storage plugin for a `scheme://` path, or the
// local filesystem storage for any other path.
func storageForPath(ctx context.Context, path string) (cedana_io.Storage, error) {
	if !strings.Contains(path, "://") {
		return &filesystem.Storage{}, nil
	}
	pluginName := fmt.Sprintf("storage/%s", strings.Split(path, "://")[0])
	var storage cedana_io.Storage
	err := features.Storage.IfAvailable(func(name string, newPluginStorage func(context.Context) (cedana_io.Storage, error)) error {
		if newPluginStorage == nil {
			return fmt.Errorf("plugin '%s' does not implement '%s'", name, features.Storage)
		}
		var err error
		storage, err = newPluginStorage(ctx)
		return err
	}, pluginName)
	if err != nil {
		return nil, err
	}
	if storage == nil {
		return nil, fmt.Errorf("plugin '%s' is not available", pluginName)
	}
	return storage, nil
}
