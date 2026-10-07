package filesystem

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	criu_proto "buf.build/gen/go/cedana/criu/protocolbuffers/go/criu"
	"github.com/cedana/cedana/pkg/config"
	criu_client "github.com/cedana/cedana/pkg/criu"
	"github.com/cedana/cedana/pkg/io"
	"github.com/cedana/cedana/pkg/profiling"
	"github.com/cedana/cedana/pkg/types"
	"github.com/cedana/cedana/pkg/utils"
	"github.com/rs/zerolog/log"
	"github.com/spf13/afero"
	"golang.org/x/sys/unix"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

const DUMP_DIR_PERMS = 0o755

// This adapter uses the provided storage to setup the dump.
// Compresses the dump directory post-dump, based on a compression format:
//   - "none" does not compress the dump directory
//   - "tar" creates a tarball of the dump directory
//   - "gzip" creates a gzipped tarball of the dump directory
//   - "lz4" creates an lz4-compressed tarball of the dump directory
//
// directoryChecksums serialises the reads of checkpoint directories for their
// checksums: one at a time on a node, so two do not compete for the same disk
var directoryChecksums sync.Mutex

func DumpFilesystem(next types.Dump) types.Dump {
	return func(ctx context.Context, opts types.Opts, resp *daemon.DumpResp, req *daemon.DumpReq) (code func() <-chan int, err error) {
		storage := opts.Storage
		dir := req.Dir
		compression := req.Compression

		if compression == "" {
			compression = config.Global.Checkpoint.Compression
		}

		if _, ok := io.SUPPORTED_COMPRESSIONS[compression]; !ok {
			return nil, status.Errorf(codes.Unimplemented, "unsupported compression format '%s'", compression)
		}

		// if compression we use a tmp dir for CRIU and then later on create compressed
		// file with storage, else ask the storage medium for a path
		var cleanup func(bool) error
		var imagesDirectory string

		if (compression != "" && compression != "none") || storage.IsRemote() {
			dir = os.TempDir()
			imagesDirectory = filepath.Join(dir, req.Name)
		} else {
			imagesDirectory, cleanup, err = storage.CreatePath(ctx, req.Dir, req.Name)
			if err != nil {
				return nil, status.Errorf(codes.Internal, "could not get path for checkpoint: %v", err)
			}
			if cleanup != nil {
				defer func() {
					cancel := false
					if err != nil {
						cancel = true
					}
					err = errors.Join(err, cleanup(cancel))
				}()
			}
		}

		// The asynchronous mode applies to every storage: the compress of a local tarball
		// runs after the dump has returned, so its hash runs outside the freeze
		async := req.Async || config.Global.Checkpoint.Async

		// Create the directory
		if err := os.Mkdir(imagesDirectory, DUMP_DIR_PERMS); err != nil {
			return nil, status.Errorf(codes.Internal, "failed to create dump dir: %v", err)
		}
		defer func() {
			if err != nil || (storage.IsRemote() && !async) {
				os.RemoveAll(imagesDirectory)
			}
		}()
		err = os.Chmod(imagesDirectory, DUMP_DIR_PERMS) // XXX: Because for some reason mkdir is not applying perms
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to chmod dump dir: %v", err)
		}

		f, err := os.Open(imagesDirectory)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to open dump dir: %v", err)
		}
		defer f.Close()

		if req.GetCriu() == nil {
			req.Criu = &criu_proto.CriuOpts{}
		}

		req.Criu.ImagesDir = proto.String(imagesDirectory)
		req.Criu.ImagesDirFd = proto.Int32(int32(f.Fd()))

		// Setup dump fs that can be used by future adapters to directly read write/extra files
		// to the dump directory
		opts.DumpFs = afero.NewBasePathFs(afero.NewOsFs(), imagesDirectory)

		// If remote storage, or compression needs to be done, we do it in CRIU's post-dump hook
		// so that if we fail compression/upload, CRIU can still resume the process (only if leave-running is not set)

		if storage.IsRemote() || (compression != "" && compression != "none") {
			ext, err := io.ExtForCompression(compression)
			if err != nil {
				return nil, err
			}
			path := req.Dir + "/" + req.Name + ".tar" + ext // do not use filepath.Join as it removes a slash (for remote)

			// The storage's writer knows the checksum of the tarball as stored, once closed
			compress := func(ctx context.Context) (checksum string, err error) {
				// detect FuseFs if dir is not remote and not provided by a plugin
				isFuse, err := isFuseFS(req.Dir, !storage.IsRemote() && !strings.Contains(req.Dir, "://"))
				if err != nil {
					return "", fmt.Errorf("failed to determine filesystem type: %w", err)
				}

				log.Debug().Str("path", path).Str("compression", compression).Bool("is_fuse", isFuse).Msg("starting compression of dump")

				tarball, err := storage.Create(ctx, path)
				if err != nil {
					return "", fmt.Errorf("failed to create tarball in storage: %w", err)
				}
				created := tarball
				defer func() {
					err = errors.Join(err, tarball.Close())
					if err == nil {
						checksum = io.ChecksumOfWriter(created)
					}
				}()

				log.Debug().Str("path", path).Str("compression", compression).Msg("creating tarball")

				tarball = profiling.IOCategory(ctx, tarball, "storage", io.Tar, compression)

				err = io.Tar(imagesDirectory, tarball, compression, isFuse)
				if err != nil {
					storage.Delete(ctx, path)
					os.RemoveAll(imagesDirectory)
					return "", fmt.Errorf("failed to create tarball: %w", err)
				}

				log.Debug().Str("path", path).Str("compression", compression).Msg("created tarball")

				os.RemoveAll(imagesDirectory)
				return checksum, nil
			}

			resp.Paths = append(resp.Paths, path)

			// If leave-running is requested, then we do not need to block process for compress/upload, because the process
			// will continue running regardless of the success of the dump/compress/upload. If leave-running is not set,
			// then we need to ensure that the dump is compressed/uploaded in the post-dump hook so that it
			// can be resumed on failure.
			//
			// When async, the response is returned before the compress/upload is complete, so the
			// path is marked as pending on the response and the outcome is available from the
			// upload's result once it has ended.

			if async {
				defer func() {
					if err != nil {
						return
					}

					// Directly add profiling data here since the compress/upload
					// will happen asynchronously and we cannot hook that into profiling later.
					size := utils.SizeFromPath(imagesDirectory)
					profiling.AddIO(ctx, size)

					// Use a detached context for async upload since the parent request
					// context will be canceled after the dump completes.
					compressCtx := context.WithoutCancel(ctx)

					finish := opts.Uploads.Start(path)
					resp.Pending = append(resp.Pending, path)

					opts.WG.Go(func() {
						log.Info().Msg("async dump compress/upload started")
						if checksum, compressErr := compress(compressCtx); compressErr != nil {
							log.Error().Err(compressErr).Msg("async compress/upload failed")
							finish("", compressErr)
						} else {
							log.Info().Str("checksum", checksum).Msg("async dump compress/upload completed")
							finish(checksum, nil)
						}
					})
				}()
			} else {
				if req.GetCriu().GetLeaveRunning() {
					defer func() {
						checksum, compressErr := compress(ctx)
						err = errors.Join(err, compressErr)
						// A synchronous dump's checksum is known when it returns; it is kept where
						// an upload's outcome is kept, so a caller learns it the same way
						if err == nil && checksum != "" && opts.Uploads != nil {
							opts.Uploads.Record(path, checksum)
							resp.Pending = append(resp.Pending, path)
						}
					}()
				} else {
					callback := &criu_client.NotifyCallback{
						PostDumpFunc: func(ctx context.Context, _ *criu_proto.CriuOpts) error {
							checksum, err := compress(ctx)
							if err == nil && checksum != "" && opts.Uploads != nil {
								// As for a leave-running dump: the checksum is known when the dump returns
								opts.Uploads.Record(path, checksum)
								resp.Pending = append(resp.Pending, path)
							}
							return err
						},
					}
					opts.CRIUCallback.Include(callback)
				}
			}
		} else {
			// Nothing else to do, just set the path and
			// add profiling data manually as no IO could be measured
			defer func() {
				size := utils.SizeFromPath(imagesDirectory)
				profiling.AddIO(ctx, size)
			}()

			// If imagesDirectory was provided by a plugin
			// dump path to be req.Dir + req.Name
			var dirPath string
			if strings.Contains(req.Dir, "://") {
				dirPath = req.Dir + req.Name
			} else {
				dirPath = imagesDirectory
			}
			resp.Paths = append(resp.Paths, dirPath)

			// CRIU writes the files itself, so no writer saw the bytes: the storage reads
			// the directory for its checksum after the dump has returned, one at a time
			// on this node, and the caller learns it the way it learns an upload's outcome
			if pc, ok := storage.(io.PathChecksummer); ok && config.Global.Checkpoint.Checksum && opts.Uploads != nil {
				finish := opts.Uploads.Start(dirPath)
				resp.Pending = append(resp.Pending, dirPath)
				checksumCtx := context.WithoutCancel(ctx)
				// The read starts once the dump has returned, when the files are complete
				defer func() {
					if err != nil {
						finish("", err)
						return
					}
					opts.WG.Go(func() {
						directoryChecksums.Lock()
						defer directoryChecksums.Unlock()
						sum, err := pc.ChecksumPath(checksumCtx, dirPath)
						if err != nil {
							log.Error().Err(err).Str("path", dirPath).Msg("could not checksum the checkpoint directory")
						}
						finish(sum, err)
					})
				}()
			}
		}

		return next(ctx, opts, resp, req)
	}
}

func isFuseFS(path string, stat bool) (bool, error) {
	if !stat {
		return false, nil
	}
	var statfs unix.Statfs_t
	if err := unix.Statfs(path, &statfs); err != nil {
		return false, fmt.Errorf("failed to get statfs for %s: %w", path, err)
	}

	// FUSE magic number is 0x65735546
	// https://github.com/torvalds/linux/blob/master/include/uapi/linux/magic.h#L39
	const FUSE_SUPER_MAGIC = 0x65735546
	if statfs.Type == FUSE_SUPER_MAGIC {
		return true, nil
	}

	return false, nil
}
