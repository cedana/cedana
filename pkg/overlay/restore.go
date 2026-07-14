// Package overlay provides shared helpers for restoring a container's
// overlayfs RW (upper) layer from a Cedana dump.
//
// It lives in the parent module (not under plugins/runc, which is a git
// submodule whose contents are not shipped when github.com/cedana/cedana is
// consumed as a versioned dependency) so that the runc plugin, the containerd
// plugin, AND the containerd runtime shim (cedana-containerd-runtime) can all
// populate an overlay upperdir from the same code.
//
// IMPORTANT (overlayfs coherence): the Linux overlayfs contract states that
// modifying the underlying upper/lower directories of a *mounted* overlay is
// undefined behavior. In particular, a file created directly in the upperdir
// after the merged parent directory has already been cached is not guaranteed
// to become visible through the overlay mount. The containerd restore path
// therefore populates the upperdir *before* the shim mounts the overlay; the
// runc path (standalone) populates it inside a CRIU pre-restore callback and
// relies on the merged tree not having been traversed yet. Either way, callers
// must not populate a live, already-traversed overlay's upperdir.
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
// later, redundant restore attempt (e.g. the runc plugin running in the shim
// subprocess after the containerd plugin already populated the upper pre-mount)
// to skip and avoid a double-populate.
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

// RestoreToUpperDir reads the RW-layer batches from the dump filesystem and
// writes them into upperDir. It is the shared core of the overlay RW-layer
// restore, decoupled from any live mount discovery so it can run either before
// or after the overlay is mounted.
//
// If the dump contains no RW-layer manifest, it is a no-op. The operation is
// idempotent with respect to symlinks, devices and named pipes (an existing
// target is replaced) so that a redundant second pass does not fail with
// EEXIST.
func RestoreToUpperDir(ctx context.Context, dump afero.Fs, upperDir string) (err error) {
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

	log.Debug().Bytes("manifestData", manifestData).Msg("read RW layer manifest data")

	var batchCount int
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		log.Debug().Str("manifestData", string(manifestData)).Msg("failed to parse as JSON, trying old format")
		_, err = fmt.Sscanf(string(manifestData), "%d\n", &batchCount)
		if err != nil {
			return fmt.Errorf("failed to parse manifest: %v", err)
		}
		log.Debug().Int("batches", batchCount).Msg("using old manifest format")
	} else {
		batchCount = int(manifest["total_batches"].(float64))
		log.Debug().Int("batches", batchCount).Interface("manifest", manifest).Msg("found rw layer batches from manifest (JSON format)")
	}

	for batchIdx := 0; batchIdx < batchCount; batchIdx++ {
		filePath := fmt.Sprintf(dumpRWLayerBatchFormatter, batchIdx)
		log.Debug().Str("filePath", filePath).Int("batchIdx", batchIdx).Msg("restoring from batch file")

		var inFile io.ReadCloser
		inFile, err = dump.Open(filePath)
		if err != nil {
			return fmt.Errorf("failed to open rw layer file %s: %v", filePath, err)
		}
		inFile = profiling.IORedundantComponent(ctx, inFile, filePath)
		defer inFile.Close()

		reader := bufio.NewReader(inFile)
		entryCount := 0

		for {
			metadataBytes, err := readDelimitedMessageBuffered(reader)
			if err == io.EOF {
				log.Debug().Str("batchFile", filePath).Int("entriesRestored", entryCount).Msg("finished restoring batch file")
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

			entryCount++
			totalEntries++
			mode := rwLayerFile.GetMode()
			fullPath := filepath.Join(upperDir, relPath)

			// The loader's cache is the file whose staleness this whole fix
			// targets; log it explicitly so we can confirm it is being written
			// into the upperdir (and with the expected size).
			isLoaderCache := strings.HasSuffix(relPath, "ld.so.cache")

			parentDir := filepath.Dir(fullPath)
			if parentDir != upperDir {
				if err := os.MkdirAll(parentDir, 0o755); err != nil {
					return fmt.Errorf("failed to create parent directory %s: %v", parentDir, err)
				}
			}

			fileType := mode & syscall.S_IFMT

			if fileType == syscall.S_IFLNK { // symlink
				if err := replaceExisting(fullPath); err != nil {
					return err
				}
				if err := os.Symlink(rwLayerFile.GetSymlinkTarget(), fullPath); err != nil {
					return fmt.Errorf("failed to create symlink %s: %v", fullPath, err)
				}
				log.Trace().Str("path", fullPath).Str("target", rwLayerFile.GetSymlinkTarget()).Msg("restored symlink")
			} else if fileType == syscall.S_IFBLK || fileType == syscall.S_IFCHR { // device
				if err := replaceExisting(fullPath); err != nil {
					return err
				}
				dev := unix.Mkdev(rwLayerFile.GetDevMajor(), rwLayerFile.GetDevMinor())
				if err := unix.Mknod(fullPath, rwLayerFile.GetMode(), int(dev)); err != nil {
					return fmt.Errorf("failed to create device %s: %v", fullPath, err)
				}
				log.Trace().Str("path", fullPath).Uint32("major", rwLayerFile.GetDevMajor()).Uint32("minor", rwLayerFile.GetDevMinor()).Msg("restored device")
			} else if fileType == syscall.S_IFDIR { // directory
				if err := os.MkdirAll(fullPath, 0o755); err != nil {
					return fmt.Errorf("failed to create directory %s: %v", fullPath, err)
				}
				if err := unix.Chmod(fullPath, mode&0o7777); err != nil {
					log.Warn().Err(err).Str("path", fullPath).Msg("failed to set directory permissions")
				}
				log.Trace().Str("path", fullPath).Msg("restored directory")
			} else if fileType == syscall.S_IFREG { // regular file
				outFile, err := os.OpenFile(fullPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(mode&0o7777))
				if err != nil {
					return fmt.Errorf("failed to create file %s: %v", fullPath, err)
				}
				defer outFile.Close()

				for {
					chunkBytes, err := readDelimitedMessageBuffered(reader)
					if err == io.EOF {
						break
					}
					if err != nil {
						return fmt.Errorf("failed to read chunk: %v", err)
					}

					chunkMsg := &runc_proto.RWFile{}
					if err := proto.Unmarshal(chunkBytes, chunkMsg); err != nil {
						if len(chunkBytes) > 0 {
							return fmt.Errorf("failed to unmarshal chunk: %v", err)
						}
						break
					}

					if len(chunkMsg.GetContent()) == 0 {
						break
					}

					for _, chunk := range chunkMsg.GetContent() {
						if _, err := outFile.Write(chunk); err != nil {
							return fmt.Errorf("failed to write content: %v", err)
						}
					}
				}

				if isLoaderCache {
					if st, serr := os.Stat(fullPath); serr != nil {
						log.Warn().Err(serr).Str("path", fullPath).Msg("loader cache missing right after write")
					} else {
						log.Info().Str("path", fullPath).Int64("size", st.Size()).Msg("wrote loader cache (ld.so.cache) into upperdir")
					}
				}

				log.Trace().Str("path", fullPath).Msg("restored regular file")
			} else if fileType == syscall.S_IFIFO { // named pipe
				if err := replaceExisting(fullPath); err != nil {
					return err
				}
				if err := unix.Mkfifo(fullPath, mode&0o7777); err != nil {
					return fmt.Errorf("failed to create named pipe %s: %v", fullPath, err)
				}
				log.Trace().Str("path", fullPath).Msg("restored named pipe")
			} else {
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

			if rwLayerFile.GetMtime() > 0 {
				mtime := time.Unix(0, int64(rwLayerFile.GetMtime()))
				if err := os.Chtimes(fullPath, mtime, mtime); err != nil {
					log.Warn().Err(err).Str("path", fullPath).Msg("failed to set mtime")
				}
			}
		}
	}

	unix.Sync()
	log.Debug().Msg("synced filesystem after RW layer restore")

	return nil
}

// replaceExisting removes a filesystem object at path if it already exists, so
// that a subsequent create (symlink/device/fifo) does not fail with EEXIST.
// This keeps RestoreToUpperDir idempotent across a redundant second pass.
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
