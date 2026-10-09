package filesystem

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	criu_proto "buf.build/gen/go/cedana/criu/protocolbuffers/go/criu"
	"github.com/cedana/cedana/internal/cedana/verify"
	"github.com/cedana/cedana/pkg/io"
	"github.com/cedana/cedana/pkg/profiling"
	"github.com/cedana/cedana/pkg/types"
	"github.com/cedana/cedana/pkg/utils"
	"github.com/rs/zerolog/log"
	"github.com/spf13/afero"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// This adapter decompresses (if required) the dump to a temporary directory for restore.
// Automatically detects the compression format from the file extension.
// When the request carries the checksum recorded at dump, the adapter verifies the
// checkpoint before the restore: a tarball as it is read, a local directory by one
// read of its files. See package verify for what a mismatch does.
func RestoreFilesystem(next types.Restore) types.Restore {
	return func(ctx context.Context, opts types.Opts, resp *daemon.RestoreResp, req *daemon.RestoreReq) (code func() <-chan int, err error) {
		storage := opts.Storage
		path := req.GetPath()
		check := verify.For(req)

		var isDir bool
		var imagesDirectory string

		path, cleanup, err := storage.ReadPath(ctx, path)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to ReadPath: %v", err)
		}

		if cleanup != nil {
			defer func() {
				err = errors.Join(err, cleanup())
			}()
		}

		if !storage.IsRemote() {
			stat, err := os.Stat(path)
			if err != nil {
				return nil, status.Errorf(codes.NotFound, "path error: %s", path)
			}
			isDir = stat.IsDir()
		}

		// If not remote storage, and path is directory, we can directly use it for CRIU

		if !storage.IsRemote() && isDir {
			imagesDirectory = path
			// Add profiling data manually as no IO can be measured
			size := utils.SizeFromPath(imagesDirectory)
			profiling.AddIO(ctx, size)

			// CRIU reads the files itself, so the directory is read once for its checksum
			if check != nil {
				sum, err := checksumOfPath(imagesDirectory)
				if err != nil {
					log.Warn().Err(err).Str("path", imagesDirectory).Msg("could not read the checkpoint directory for its checksum, not verified")
				} else if err := check.Record(resp, check.Compare(sum, "", true)); err != nil {
					return nil, err
				}
			}
		} else {
			// Create a temporary directory for the restore
			imagesDirectory = filepath.Join(os.TempDir(), fmt.Sprintf("restore-%d", time.Now().UnixNano()))

			if err := os.Mkdir(imagesDirectory, DUMP_DIR_PERMS); err != nil {
				return nil, status.Errorf(codes.Internal, "failed to create restore dir: %v", err)
			}
			err = os.Chmod(imagesDirectory, DUMP_DIR_PERMS) // XXX: Because for some reason mkdir is not applying perms
			if err != nil {
				return nil, status.Errorf(codes.Internal, "failed to chmod restore dir: %v", err)
			}
			defer os.RemoveAll(imagesDirectory)

			// The store's own value of the object, compared before the download: a value
			// that differs says the stored bytes are bad without transferring them
			var store string
			if check != nil {
				store = verify.StoreValue(ctx, storage, path)
				if check.StoreDiffers(store) && check.Strict {
					return nil, check.Record(resp, check.Stored(store))
				}
			}

			decompress := func(ctx context.Context) (sum string, err error) {
				// Detect compression from path
				compression, err := io.CompressionFromExt(path)
				if err != nil {
					return "", err
				}

				tarball, err := storage.Open(ctx, path)
				if err != nil {
					return "", fmt.Errorf("failed to open dump file: %v", err)
				}
				defer func() {
					err = errors.Join(err, tarball.Close())
				}()

				var hashed *io.ChecksumReader
				if check != nil {
					hashed = io.NewChecksumReader(tarball)
					tarball = hashed
				}

				log.Debug().Str("path", path).Str("compression", compression).Msg("decompressing tarball")

				tarball = profiling.IOCategory(ctx, tarball, "storage", io.Untar, compression)
				err = io.Untar(tarball, imagesDirectory, compression)
				if err != nil {
					return "", fmt.Errorf("failed to decompress dump: %v", err)
				}

				// The tar and compression readers can stop before the end of the file
				if hashed != nil {
					if err := hashed.Drain(); err != nil {
						return "", fmt.Errorf("failed to read dump file: %v", err)
					}
					sum = hashed.Sum()
				}

				log.Debug().Str("path", path).Str("compression", compression).Msg("decompressed tarball")

				return sum, nil
			}

			sum, err := decompress(ctx)
			if err != nil {
				return nil, status.Error(codes.Internal, err.Error())
			}

			if check != nil {
				local := !storage.IsRemote()
				outcome := check.Compare(sum, store, local)
				// The stored bytes may be right and the read wrong: read once more
				if check.Retry(outcome) {
					log.Warn().Str("reason", verify.ReasonName(outcome.Reason)).Msg("checkpoint checksum mismatch, reading the checkpoint once more")
					if err := emptyDir(imagesDirectory); err != nil {
						return nil, status.Errorf(codes.Internal, "failed to clear restore dir: %v", err)
					}
					if sum, err = decompress(ctx); err != nil {
						return nil, status.Error(codes.Internal, err.Error())
					}
					outcome = check.Compare(sum, store, local)
				}
				if err := check.Record(resp, outcome); err != nil {
					return nil, err
				}
			}
		}

		dir, err := os.Open(imagesDirectory)
		if err != nil {
			os.RemoveAll(imagesDirectory)
			return nil, status.Errorf(codes.Internal, "failed to open dump dir: %v", err)
		}
		defer dir.Close()

		if req.GetCriu() == nil {
			req.Criu = &criu_proto.CriuOpts{}
		}

		req.Criu.ImagesDir = proto.String(imagesDirectory)
		req.Criu.ImagesDirFd = proto.Int32(int32(dir.Fd()))

		// Setup dump fs that can be used by future adapters to directly read files
		// to the dump directory
		opts.DumpFs = afero.NewBasePathFs(afero.NewOsFs(), imagesDirectory)

		return next(ctx, opts, resp, req)
	}
}

// emptyDir removes everything in a directory and keeps the directory
func emptyDir(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := os.RemoveAll(filepath.Join(dir, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}
