// Package overlay provides shared helpers for restoring a container's
// overlayfs RW (upper) layer from a dump.
//
// It lives in the parent module so that runc plugin, the containerd
// plugin, and the containerd runtime shim (cedana-containerd-runtime) can all
// use it.
//
// The Linux overlayfs contract states that modifying the underlying
// upper/lower directories of a *mounted* overlay is undefined behavior.
// Therefore the RW layer must be restored into the upperdir *before* the
// overlay is mounted, and only the component that performs the mount can do
// that: the containerd shim (cedana-containerd-runtime), which populates the
// upperdir just before mount.All on every restore path (Kubernetes and CLI
// alike). It drops a marker so the runc plugin's post-mount restore is a
// no-op.
//
// The runc plugin's post-mount restore remains as a fallback for the cases
// the shim cannot handle (no readable checkpoint directory, e.g. streamed
// dumps whose RW layer lives inside stream shards, or plain runc restores
// where the overlay was mounted by a third party).
package overlay

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	runc_proto "buf.build/gen/go/cedana/cedana/protocolbuffers/go/plugins/runc"
	"github.com/cedana/cedana/pkg/profiling"
	securejoin "github.com/cyphar/filepath-securejoin"
	"github.com/rs/zerolog/log"
	"github.com/spf13/afero"
	"golang.org/x/sys/unix"
	"google.golang.org/protobuf/proto"
)

// RW-layer dump file names. This is the single source of truth: the dump side
// (plugins/runc) and every restore site (runc plugin, containerd plugin, and
// the containerd runtime shim) reference these constants.
const (
	DumpRWLayerManifestKey    = "rw-layer.manifest"
	DumpRWLayerBatchFormatter = "rw-layer-%d.img"
)

// RestoredMarker is dropped in the snapshot directory (the parent of the
// upperdir, which is outside the overlay's merged view and therefore invisible
// to the container) once the RW layer has been restored. Its presence tells a
// later, redundant restore attempt to skip and avoid a double-populate.
const RestoredMarker = ".cedana-rw-restored"

func MarkerPath(upperDir string) string {
	return filepath.Join(filepath.Dir(upperDir), RestoredMarker)
}

func AlreadyRestored(upperDir string) bool {
	_, err := os.Lstat(MarkerPath(upperDir))
	return err == nil
}

// WriteMarker records that the RW layer for the given upperdir has been
// restored.
func WriteMarker(upperDir string) error {
	return os.WriteFile(MarkerPath(upperDir), nil, 0o600)
}

// UpperDirFromMountOptions extracts the upperdir from a set of overlay mount
// options
func UpperDirFromMountOptions(options []string) (string, error) {
	for _, opt := range options {
		if after, ok := strings.CutPrefix(opt, "upperdir="); ok {
			return after, nil
		}
	}
	return "", fmt.Errorf("upperdir not found in overlay mount options")
}

// RestoreToUpperDir reads the RW-layer batches from the dump filesystem and
// writes them into upperDir. Returns restored=false (and no error) if the
// dump has no readable RW-layer manifest — e.g. the checkpoint has no RW
// layer at all, or it is a streamed dump whose RW layer lives inside stream
// shards. Callers must only drop the restored marker when restored is true,
// otherwise the post-mount fallback (which may have a streaming-capable dump
// filesystem) would be skipped and the RW layer silently lost.
func RestoreToUpperDir(ctx context.Context, dump afero.Fs, upperDir string) (restored bool, err error) {
	manifestFile, err := dump.Open(DumpRWLayerManifestKey)
	if err != nil {
		log.Debug().Err(err).Msg("no RW layer manifest found, skipping restore")
		return false, nil
	}
	return true, restoreFromManifest(ctx, dump, upperDir, manifestFile)
}

// restoreFromManifest reads the RW-layer batches listed in the manifest and
// writes their entries into upperDir. Closes manifestFile.
func restoreFromManifest(ctx context.Context, dump afero.Fs, upperDir string, manifestFile io.ReadCloser) (err error) {
	manifestFile = profiling.IORedundantComponent(ctx, manifestFile, DumpRWLayerManifestKey)
	defer manifestFile.Close()

	totalEntries := 0
	log.Info().Str("upperDir", upperDir).Msg("RW layer restore starting")
	defer func() {
		if err != nil {
			log.Error().Err(err).Str("upperDir", upperDir).Msg("RW layer restore failed")
		} else {
			log.Info().Str("upperDir", upperDir).Int("entries", totalEntries).Msg("RW layer restore complete")
		}
	}()

	var manifest map[string]any
	manifestData, err := io.ReadAll(manifestFile)
	if err != nil {
		return fmt.Errorf("failed to read manifest: %v", err)
	}

	var batchCount int
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		if _, err = fmt.Sscanf(string(manifestData), "%d\n", &batchCount); err != nil || batchCount < 0 {
			return fmt.Errorf("failed to parse manifest (invalid batch count): %v", err)
		}
	} else {
		tb, ok := manifest["total_batches"].(float64)
		if !ok || tb < 0 || tb != math.Trunc(tb) || tb > math.MaxInt32 {
			return fmt.Errorf("manifest total_batches is missing or not a non-negative integer: %v", manifest["total_batches"])
		}
		batchCount = int(tb)
	}
	log.Debug().Int("batches", batchCount).Msg("found rw layer batches from manifest")

	for batchIdx := 0; batchIdx < batchCount; batchIdx++ {
		filePath := fmt.Sprintf(DumpRWLayerBatchFormatter, batchIdx)

		var inFile io.ReadCloser
		inFile, err = dump.Open(filePath)
		if err != nil {
			return fmt.Errorf("failed to open rw layer file %s: %v", filePath, err)
		}
		inFile = profiling.IORedundantComponent(ctx, inFile, filePath)
		defer inFile.Close()

		reader := bufio.NewReader(inFile)

		for {
			metadataBytes, err := readDelimitedMessageBuffered(reader)
			if err == io.EOF {
				break
			}
			if err != nil {
				return fmt.Errorf("failed to read metadata from %s: %v", filePath, err)
			}

			rwLayerFile := &runc_proto.RWFile{}
			if err := proto.Unmarshal(metadataBytes, rwLayerFile); err != nil {
				return fmt.Errorf("failed to unmarshal metadata from %s: %v", filePath, err)
			}

			relPath := rwLayerFile.GetPath()
			if relPath == "" || relPath == "." {
				continue
			}

			totalEntries++
			mode := rwLayerFile.GetMode()
			// SecureJoin clamps the entry within upperDir and safely resolves any
			// symlink components
			fullPath, serr := securejoin.SecureJoin(upperDir, relPath)
			if serr != nil {
				return fmt.Errorf("failed to resolve safe path for %q: %v", relPath, serr)
			}

			parentDir := filepath.Dir(fullPath)
			if parentDir != upperDir {
				if err := os.MkdirAll(parentDir, 0o755); err != nil {
					return fmt.Errorf("failed to create parent directory %s: %v", parentDir, err)
				}
			}

			fileType := mode & syscall.S_IFMT

			switch fileType {
			case syscall.S_IFLNK: // symlink
				if err := replaceExisting(fullPath); err != nil {
					return err
				}
				if err := os.Symlink(rwLayerFile.GetSymlinkTarget(), fullPath); err != nil {
					return fmt.Errorf("failed to create symlink %s: %v", fullPath, err)
				}
			case syscall.S_IFBLK, syscall.S_IFCHR: // device / whiteout
				if err := replaceExisting(fullPath); err != nil {
					return err
				}
				dev := unix.Mkdev(rwLayerFile.GetDevMajor(), rwLayerFile.GetDevMinor())
				if err := unix.Mknod(fullPath, rwLayerFile.GetMode(), int(dev)); err != nil {
					return fmt.Errorf("failed to create device %s: %v", fullPath, err)
				}
			case syscall.S_IFDIR: // directory
				if err := os.MkdirAll(fullPath, 0o755); err != nil {
					return fmt.Errorf("failed to create directory %s: %v", fullPath, err)
				}
				if err := unix.Chmod(fullPath, mode&0o7777); err != nil {
					log.Warn().Err(err).Str("path", fullPath).Msg("failed to set directory permissions")
				}
			case syscall.S_IFREG: // regular file
				// Remove any existing object first, then create with O_NOFOLLOW,
				// so a symlink restored by an earlier entry can't cause this write
				// to truncate/overwrite the link's target.
				if err := replaceExisting(fullPath); err != nil {
					return err
				}
				outFile, err := os.OpenFile(fullPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC|unix.O_NOFOLLOW, os.FileMode(mode&0o7777))
				if err != nil {
					return fmt.Errorf("failed to create file %s: %v", fullPath, err)
				}
				if err := writeFileContent(reader, outFile); err != nil {
					outFile.Close()
					return err
				}
				outFile.Close()
			case syscall.S_IFIFO: // named pipe
				if err := replaceExisting(fullPath); err != nil {
					return err
				}
				if err := unix.Mkfifo(fullPath, mode&0o7777); err != nil {
					return fmt.Errorf("failed to create named pipe %s: %v", fullPath, err)
				}
			default:
				log.Warn().Str("path", fullPath).Uint32("mode", mode).Msg("unsupported file type during restore")
				continue
			}

			if err := os.Lchown(fullPath, int(rwLayerFile.GetUid()), int(rwLayerFile.GetGid())); err != nil {
				log.Warn().Err(err).Str("path", fullPath).Msg("failed to set ownership")
			}

			if mode&syscall.S_IFLNK == 0 {
				if err := unix.Chmod(fullPath, mode&0o7777); err != nil {
					log.Warn().Err(err).Str("path", fullPath).Msg("failed to set permissions")
				}
			}

			for name, encodedValue := range rwLayerFile.GetXattrs() {
				value, err := base64.StdEncoding.DecodeString(encodedValue)
				if err != nil {
					log.Warn().Err(err).Str("xattr", name).Str("path", fullPath).Msg("failed to decode xattr")
					continue
				}
				if err := unix.Lsetxattr(fullPath, name, value, 0); err != nil {
					log.Warn().Err(err).Str("xattr", name).Str("path", fullPath).Msg("failed to set xattr")
				}
			}

			if rwLayerFile.GetMtime() > 0 && fileType != syscall.S_IFLNK {
				mtime := time.Unix(0, int64(rwLayerFile.GetMtime()))
				if err := os.Chtimes(fullPath, mtime, mtime); err != nil {
					log.Warn().Err(err).Str("path", fullPath).Msg("failed to set mtime")
				}
			}
		}
	}

	// No global sync(2) here: it waits for every dirty page on the node (a fresh
	// checkpoint alone can be hundreds of GB) and the overlay mount that follows
	// reads through the page cache regardless. Durability of the upperdir is not
	// a restore requirement.
	return nil
}

// writeFileContent reads chunk messages from reader and writes their content to
// out, until the terminating empty chunk (or EOF).
func writeFileContent(reader *bufio.Reader, out io.Writer) error {
	for {
		chunkBytes, err := readDelimitedMessageBuffered(reader)
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("failed to read chunk: %v", err)
		}

		chunkMsg := &runc_proto.RWFile{}
		if err := proto.Unmarshal(chunkBytes, chunkMsg); err != nil {
			if len(chunkBytes) > 0 {
				return fmt.Errorf("failed to unmarshal chunk: %v", err)
			}
			return nil
		}

		if len(chunkMsg.GetContent()) == 0 {
			return nil
		}

		for _, chunk := range chunkMsg.GetContent() {
			if _, err := out.Write(chunk); err != nil {
				return fmt.Errorf("failed to write content: %v", err)
			}
		}
	}
}

// removes a filesystem object at path if it already exists, so
// that a subsequent create (symlink/device/fifo) does not fail with EEXIST.
func replaceExisting(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("failed to replace existing path %s: %v", path, err)
	}
	return nil
}

func readDelimitedMessageBuffered(r *bufio.Reader) ([]byte, error) {
	var size uint32
	if err := binary.Read(r, binary.LittleEndian, &size); err != nil {
		return nil, err
	}
	data := make([]byte, size)
	_, err := io.ReadFull(r, data)
	return data, err
}
