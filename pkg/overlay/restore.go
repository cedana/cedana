// Package overlay provides shared helpers for restoring a container's
// overlayfs RW (upper) layer from a Cedana dump.
//
// It lives in the parent module (not under plugins/runc, which is a git
// submodule whose contents are not shipped when github.com/cedana/cedana is
// consumed as a versioned dependency) so that the runc plugin, the containerd
// plugin, AND the containerd runtime shim (cedana-containerd-runtime) can all
// use it.
//
// IMPORTANT (overlayfs coherence): the Linux overlayfs contract states that
// modifying the underlying upper/lower directories of a *mounted* overlay is
// undefined behavior. A file created directly in the upperdir after the merged
// parent directory has already been cached is not guaranteed to become visible
// through the overlay mount. There are two safe ways to restore the RW layer:
//
//   - Before the overlay is mounted: write straight into the upperdir
//     (RestoreToUpperDir). Only valid pre-mount.
//   - After the overlay is mounted: write THROUGH the merged mount
//     (RestoreThroughMount). The kernel performs the copy-up itself so its
//     dcache stays coherent. This is the only safe option once the overlay is
//     already mounted (e.g. the runc plugin running under the containerd shim
//     for a Kubernetes pod).
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
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	runc_proto "buf.build/gen/go/cedana/cedana/protocolbuffers/go/plugins/runc"
	"github.com/cedana/cedana/pkg/profiling"
	"github.com/rs/zerolog/log"
	"github.com/spf13/afero"
	"golang.org/x/sys/unix"
	"google.golang.org/protobuf/proto"
)

// RW-layer dump file names. These mirror the runc plugin's keys
// (plugins/runc/pkg/keys) but are inlined here so this package has no
// dependency on the plugins/runc submodule, keeping it importable by the
// containerd runtime shim as a plain module dependency.
const (
	dumpRWLayerManifestKey    = "rw-layer.manifest"
	dumpRWLayerBatchFormatter = "rw-layer-%d.img"
)

// RestoredMarker is dropped in the snapshot directory (the parent of the
// upperdir, which is outside the overlay's merged view and therefore invisible
// to the container) once the RW layer has been restored. Its presence tells a
// later, redundant restore attempt to skip and avoid a double-populate.
const RestoredMarker = ".cedana-rw-restored"

// MarkerPath returns the path of the restored-marker for a given upperdir.
func MarkerPath(upperDir string) string {
	return filepath.Join(filepath.Dir(upperDir), RestoredMarker)
}

// AlreadyRestored reports whether the RW layer for the given upperdir has
// already been restored (i.e. the marker is present).
func AlreadyRestored(upperDir string) bool {
	_, err := os.Lstat(MarkerPath(upperDir))
	return err == nil
}

// WriteMarker records that the RW layer for the given upperdir has been
// restored. A failure to write the marker is not fatal; it only risks a
// redundant (idempotent) second restore pass.
func WriteMarker(upperDir string) error {
	return os.WriteFile(MarkerPath(upperDir), nil, 0o600)
}

// UpperDirFromMountOptions extracts the upperdir from a set of overlay mount
// options (e.g. those returned by a containerd snapshotter's Mounts call).
// Returns an error if there is no upperdir option (e.g. a read-only view mount
// or a non-overlay filesystem).
func UpperDirFromMountOptions(options []string) (string, error) {
	for _, opt := range options {
		if after, ok := strings.CutPrefix(opt, "upperdir="); ok {
			return after, nil
		}
	}
	return "", fmt.Errorf("upperdir not found in overlay mount options")
}

// RestoreToUpperDir writes the RW layer straight into upperDir. This is ONLY
// coherent if the overlay is not yet mounted (pre-mount population). If the
// overlay is already mounted, use RestoreThroughMount instead.
func RestoreToUpperDir(ctx context.Context, dump afero.Fs, upperDir string) error {
	return restore(ctx, dump, upperDir, false)
}

// RestoreThroughMount writes the RW layer THROUGH the merged overlay mount at
// root (the mounted rootfs). The kernel performs copy-up, so the merged view is
// coherent immediately. This is the correct way to restore into an
// already-mounted overlay. Whiteouts (character device 0/0 in the dump) are
// reproduced by deleting the corresponding path through the mount, which makes
// overlayfs create the whiteout.
func RestoreThroughMount(ctx context.Context, dump afero.Fs, root string) error {
	return restore(ctx, dump, root, true)
}

// restore reads the RW-layer batches from the dump filesystem and writes them
// under targetRoot. When throughMount is true, targetRoot is a live overlay
// mount: whiteouts are reproduced via delete and overlay-internal xattrs are
// skipped. When false, targetRoot is a raw upperdir and entries are written
// verbatim (whiteouts as char 0/0 devices).
//
// If the dump contains no RW-layer manifest, it is a no-op. The operation is
// idempotent (existing symlinks/devices/pipes are replaced) so a redundant
// second pass does not fail with EEXIST.
func restore(ctx context.Context, dump afero.Fs, targetRoot string, throughMount bool) (err error) {
	var manifestFile io.ReadCloser
	manifestPath := dumpRWLayerManifestKey
	manifestFile, err = dump.Open(manifestPath)
	if err != nil {
		log.Debug().Err(err).Msg("no RW layer manifest found, skipping restore")
		return nil
	}
	manifestFile = profiling.IORedundantComponent(ctx, manifestFile, manifestPath)
	defer manifestFile.Close()

	totalEntries := 0
	log.Info().Str("target", targetRoot).Bool("throughMount", throughMount).Msg("RW layer restore starting")
	defer func() {
		if err != nil {
			log.Error().Err(err).Str("target", targetRoot).Msg("RW layer restore failed")
		} else {
			log.Info().Str("target", targetRoot).Int("entries", totalEntries).Msg("RW layer restore complete")
		}
	}()

	var manifest map[string]any
	manifestData, err := io.ReadAll(manifestFile)
	if err != nil {
		return fmt.Errorf("failed to read manifest: %v", err)
	}

	var batchCount int
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		if _, err = fmt.Sscanf(string(manifestData), "%d\n", &batchCount); err != nil {
			return fmt.Errorf("failed to parse manifest: %v", err)
		}
	} else {
		batchCount = int(manifest["total_batches"].(float64))
	}
	log.Debug().Int("batches", batchCount).Msg("found rw layer batches from manifest")

	for batchIdx := 0; batchIdx < batchCount; batchIdx++ {
		filePath := fmt.Sprintf(dumpRWLayerBatchFormatter, batchIdx)

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
			fullPath := filepath.Join(targetRoot, relPath)
			isLoaderCache := strings.HasSuffix(relPath, "ld.so.cache")

			parentDir := filepath.Dir(fullPath)
			if parentDir != targetRoot {
				if err := os.MkdirAll(parentDir, 0o755); err != nil {
					return fmt.Errorf("failed to create parent directory %s: %v", parentDir, err)
				}
			}

			fileType := mode & syscall.S_IFMT
			isWhiteout := fileType == syscall.S_IFCHR && rwLayerFile.GetDevMajor() == 0 && rwLayerFile.GetDevMinor() == 0

			// A whiteout in the dump means the container deleted a path that
			// exists in a lower layer. Through the mount we reproduce that by
			// deleting the path (overlay creates the whiteout for us). Into a raw
			// upperdir we keep writing the literal char 0/0 device.
			if throughMount && isWhiteout {
				if err := os.RemoveAll(fullPath); err != nil && !errors.Is(err, os.ErrNotExist) {
					return fmt.Errorf("failed to apply whiteout %s: %v", fullPath, err)
				}
				log.Trace().Str("path", fullPath).Msg("applied whiteout (delete through mount)")
				continue
			}

			switch {
			case fileType == syscall.S_IFLNK: // symlink
				if err := replaceExisting(fullPath); err != nil {
					return err
				}
				if err := os.Symlink(rwLayerFile.GetSymlinkTarget(), fullPath); err != nil {
					return fmt.Errorf("failed to create symlink %s: %v", fullPath, err)
				}
			case fileType == syscall.S_IFBLK || fileType == syscall.S_IFCHR: // device (incl. raw whiteout in upperdir mode)
				if err := replaceExisting(fullPath); err != nil {
					return err
				}
				dev := unix.Mkdev(rwLayerFile.GetDevMajor(), rwLayerFile.GetDevMinor())
				if err := unix.Mknod(fullPath, rwLayerFile.GetMode(), int(dev)); err != nil {
					return fmt.Errorf("failed to create device %s: %v", fullPath, err)
				}
			case fileType == syscall.S_IFDIR: // directory
				if err := os.MkdirAll(fullPath, 0o755); err != nil {
					return fmt.Errorf("failed to create directory %s: %v", fullPath, err)
				}
				if err := unix.Chmod(fullPath, mode&0o7777); err != nil {
					log.Warn().Err(err).Str("path", fullPath).Msg("failed to set directory permissions")
				}
			case fileType == syscall.S_IFREG: // regular file
				outFile, err := os.OpenFile(fullPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(mode&0o7777))
				if err != nil {
					return fmt.Errorf("failed to create file %s: %v", fullPath, err)
				}

				if err := writeFileContent(reader, outFile); err != nil {
					outFile.Close()
					return err
				}
				outFile.Close()

				if isLoaderCache {
					if st, serr := os.Stat(fullPath); serr != nil {
						log.Warn().Err(serr).Str("path", fullPath).Msg("loader cache missing right after write")
					} else {
						log.Info().Str("path", fullPath).Int64("size", st.Size()).Bool("throughMount", throughMount).Msg("wrote loader cache (ld.so.cache)")
					}
				}
			case fileType == syscall.S_IFIFO: // named pipe
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
				// overlay-internal xattrs cannot (and must not) be set through a
				// live mount; the kernel manages them.
				if throughMount && strings.HasPrefix(name, "trusted.overlay.") {
					continue
				}
				value, err := base64.StdEncoding.DecodeString(encodedValue)
				if err != nil {
					log.Warn().Err(err).Str("xattr", name).Str("path", fullPath).Msg("failed to decode xattr")
					continue
				}
				if err := unix.Lsetxattr(fullPath, name, value, 0); err != nil {
					log.Warn().Err(err).Str("xattr", name).Str("path", fullPath).Msg("failed to set xattr")
				}
			}

			if rwLayerFile.GetMtime() > 0 {
				mtime := time.Unix(0, int64(rwLayerFile.GetMtime()))
				if err := os.Chtimes(fullPath, mtime, mtime); err != nil {
					log.Warn().Err(err).Str("path", fullPath).Msg("failed to set mtime")
				}
			}
		}
	}

	unix.Sync()
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

// replaceExisting removes a filesystem object at path if it already exists, so
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
