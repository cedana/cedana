package filesystem

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	containerd_proto "buf.build/gen/go/cedana/cedana/protocolbuffers/go/plugins/containerd"
	"github.com/cedana/cedana/pkg/types"
	containerd_keys "github.com/cedana/cedana/plugins/containerd/pkg/keys"
	"github.com/containerd/containerd"
	"github.com/rs/zerolog/log"
	"golang.org/x/sys/unix"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	proto_proto "google.golang.org/protobuf/proto"
)

func RestoreRWLayer(next types.Restore) types.Restore {
	return func(ctx context.Context, opts types.Opts, resp *daemon.RestoreResp, req *daemon.RestoreReq) (code func() <-chan int, err error) {
		client, ok := ctx.Value(containerd_keys.CLIENT_CONTEXT_KEY).(*containerd.Client)
		if !ok {
			return nil, status.Errorf(codes.Internal, "failed to get containerd client from context")
		}

		container, ok := ctx.Value(containerd_keys.CONTAINER_CONTEXT_KEY).(containerd.Container)
		if !ok {
			return nil, status.Errorf(codes.Internal, "failed to get container from context")
		}

		imagesDir := req.GetPath()

		log.Info().Str("container", container.ID()).Str("imagesDir", imagesDir).Msg("restoring rw layer")

		if err := restoreRWLayer(ctx, client, container, imagesDir); err != nil {
			return nil, status.Errorf(codes.Internal, "failed to restore rw layer: %v", err)
		}

		resp.Messages = append(resp.Messages, "Restored RW layer")

		return next(ctx, opts, resp, req)
	}
}

func restoreRWLayer(ctx context.Context, client *containerd.Client, container containerd.Container, imagesDir string) (err error) {
	log.Info().Str("container", container.ID()).Msg("rw layer restore started")
	defer func() {
		if err != nil {
			log.Error().Err(err).Str("container", container.ID()).Msg("rw layer restore failed")
		} else {
			log.Info().Str("container", container.ID()).Msg("rw layer restore completed")
		}
	}()

	info, err := container.Info(ctx)
	if err != nil {
		return fmt.Errorf("failed to get container info: %v", err)
	}

	snapshotter := client.SnapshotService(info.Snapshotter)

	mounts, err := snapshotter.Mounts(ctx, info.SnapshotKey)
	if err != nil {
		return fmt.Errorf("failed to get container mounts: %v", err)
	}

	var upperDir string
	for _, m := range mounts {
		if m.Type == "overlay" {
			for _, o := range m.Options {
				var found bool
				upperDir, found = strings.CutPrefix(o, "upperdir=")
				if found {
					break
				}
			}
		}
	}

	rwLayerFiles, err := filepath.Glob(filepath.Join(imagesDir, "rw-layer-*.img"))
	if err != nil {
		return fmt.Errorf("failed to glob rw layer files: %v", err)
	}
	
	log.Info().Int("batchFiles", len(rwLayerFiles)).Msg("found rw layer batch files")

	type fileEntry struct {
		path       string
		relPath    string
		mode       os.FileMode
		metadata   *containerd_proto.RWFile
		filePath   string
		isDir      bool
	}

	var entries []fileEntry

	for _, rwFilePath := range rwLayerFiles {
		inFile, err := os.Open(rwFilePath)
		if err != nil {
			return fmt.Errorf("failed to open rw layer file %s: %v", rwFilePath, err)
		}
		defer inFile.Close()

		metadataBytes, err := readDelimitedMessage(inFile)
		if err != nil {
			return fmt.Errorf("failed to read metadata from %s: %v", rwFilePath, err)
		}

		rwLayerFile := &containerd_proto.RWFile{}
		if err := proto_proto.Unmarshal(metadataBytes, rwLayerFile); err != nil {
			return fmt.Errorf("failed to unmarshal metadata from %s: %v", rwFilePath, err)
		}

		mode := os.FileMode(rwLayerFile.GetMode())
		entries = append(entries, fileEntry{
			path:     filepath.Join(upperDir, rwLayerFile.GetPath()),
			relPath:  rwLayerFile.GetPath(),
			mode:     mode,
			metadata: rwLayerFile,
			filePath: rwFilePath,
			isDir:    mode.IsDir(),
		})
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].isDir != entries[j].isDir {
			return entries[i].isDir
		}
		return strings.Count(entries[i].relPath, string(filepath.Separator)) < strings.Count(entries[j].relPath, string(filepath.Separator))
	})

	log.Info().Int("total_entries", len(entries)).Msg("sorted entries for restore")

	for _, entry := range entries {
		fullPath := entry.path
		mode := entry.mode
		rwLayerFile := entry.metadata

		log.Debug().Str("path", fullPath).Str("relPath", entry.relPath).Str("mode", mode.String()).Bool("isDir", entry.isDir).Msg("restoring entry")

		if mode&os.ModeSymlink != 0 {
			if err := os.Symlink(rwLayerFile.GetSymlinkTarget(), fullPath); err != nil {
				return fmt.Errorf("failed to create symlink %s: %v", fullPath, err)
			}
			log.Debug().Str("path", fullPath).Str("target", rwLayerFile.GetSymlinkTarget()).Msg("restored symlink")
		} else if mode&os.ModeDevice != 0 {
			dev := unix.Mkdev(rwLayerFile.GetDevMajor(), rwLayerFile.GetDevMinor())
			if err := unix.Mknod(fullPath, uint32(mode), int(dev)); err != nil {
				return fmt.Errorf("failed to create device %s: %v", fullPath, err)
			}
			log.Debug().Str("path", fullPath).Uint32("major", rwLayerFile.GetDevMajor()).Uint32("minor", rwLayerFile.GetDevMinor()).Msg("restored device")
		} else if mode.IsDir() {
			if err := os.MkdirAll(fullPath, mode&os.ModePerm); err != nil {
				return fmt.Errorf("failed to create directory %s: %v", fullPath, err)
			}
			log.Debug().Str("path", fullPath).Msg("restored directory")
		} else if mode.IsRegular() {
			inFile, err := os.Open(entry.filePath)
			if err != nil {
				return fmt.Errorf("failed to open rw layer file %s: %v", entry.filePath, err)
			}
			if _, err := readDelimitedMessage(inFile); err != nil {
				inFile.Close()
				return fmt.Errorf("failed to skip metadata from %s: %v", entry.filePath, err)
			}
			outFile, err := os.OpenFile(fullPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode&os.ModePerm)
			if err != nil {
				return fmt.Errorf("failed to create file %s: %v", fullPath, err)
			}

			for {
				chunkBytes, err := readDelimitedMessage(inFile)
				if err == io.EOF {
					break
				}
				if err != nil {
					inFile.Close()
					outFile.Close()
					return fmt.Errorf("failed to read chunk from %s: %v", entry.filePath, err)
				}

				chunkMsg := &containerd_proto.RWFile{}
				if err := proto_proto.Unmarshal(chunkBytes, chunkMsg); err != nil {
					inFile.Close()
					outFile.Close()
					return fmt.Errorf("failed to unmarshal chunk from %s: %v", entry.filePath, err)
				}

				for _, chunk := range chunkMsg.GetContent() {
					if _, err := outFile.Write(chunk); err != nil {
						inFile.Close()
						outFile.Close()
						return fmt.Errorf("failed to write content to %s: %v", fullPath, err)
					}
				}
			}
			inFile.Close()
			outFile.Close()
			log.Info().Str("path", fullPath).Str("relPath", entry.relPath).Msg("restored regular file")
		} else {
			log.Warn().Str("path", fullPath).Str("mode", mode.String()).Msg("unsupported file type during restore")
			continue
		}

		if err := os.Lchown(fullPath, int(rwLayerFile.GetUid()), int(rwLayerFile.GetGid())); err != nil {
			return fmt.Errorf("failed to set ownership for %s: %v", fullPath, err)
		}

		if mode&os.ModeSymlink == 0 {
			if err := os.Chmod(fullPath, mode&os.ModePerm); err != nil {
				return fmt.Errorf("failed to set permissions for %s: %v", fullPath, err)
			}
		}

		for name, encodedValue := range rwLayerFile.GetXattrs() {
			value, err := base64.StdEncoding.DecodeString(encodedValue)
			if err != nil {
				return fmt.Errorf("failed to decode xattr %s for %s: %v", name, fullPath, err)
			}
			if err := unix.Lsetxattr(fullPath, name, value, 0); err != nil {
				return fmt.Errorf("failed to set xattr %s for %s: %v", name, fullPath, err)
			}
		}

		if rwLayerFile.GetMtime() > 0 {
			mtime := time.Unix(0, int64(rwLayerFile.GetMtime()))
			if err := os.Chtimes(fullPath, mtime, mtime); err != nil {
				log.Warn().Err(err).Str("path", fullPath).Msg("failed to set mtime")
			}
		}

		log.Debug().Str("path", fullPath).Msg("restored file metadata")
	}

	log.Info().Str("dir", upperDir).Int("files", len(rwLayerFiles)).Msg("restored rw layer files")

	return nil
}

func readDelimitedMessage(r io.Reader) ([]byte, error) {
	var size uint32
	if err := binary.Read(r, binary.LittleEndian, &size); err != nil {
		return nil, err
	}
	data := make([]byte, size)
	_, err := io.ReadFull(r, data)
	return data, err
}
