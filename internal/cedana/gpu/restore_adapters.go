package gpu

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"buf.build/gen/go/cedana/criu/protocolbuffers/go/criu"
	"github.com/cedana/cedana/pkg/config"
	"github.com/cedana/cedana/pkg/features"
	"github.com/cedana/cedana/pkg/keys"
	"github.com/cedana/cedana/pkg/profiling"
	"github.com/cedana/cedana/pkg/types"
	"github.com/cedana/cedana/pkg/utils"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/spf13/afero"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// Adapter that restores GPU support to the request.
func Restore(gpus Manager) types.Adapter[types.Restore] {
	return func(next types.Restore) types.Restore {
		return func(ctx context.Context, opts types.Opts, resp *daemon.RestoreResp, req *daemon.RestoreReq) (code func() <-chan int, err error) {
			state := resp.GetState()

			if !state.GPUEnabled {
				return next(ctx, opts, resp, req)
			}

			if !opts.Plugins.IsInstalled("gpu") {
				return nil, status.Errorf(codes.FailedPrecondition, "Please install the GPU plugin to restore GPU support")
			}

			err = gpus.Sync(ctx)
			if err != nil {
				return nil, status.Errorf(codes.Internal, "failed to sync GPU manager: %v", err)
			}

			pid := make(chan uint32, 1)
			defer close(pid)

			var id string

			if req.GPUID != "" {
				id = req.GPUID
			} else {
				_, end := profiling.StartTimingCategory(ctx, "gpu", gpus.Attach)
				id, err = gpus.Attach(ctx, pid)
				end()
				if err != nil {
					return nil, status.Errorf(codes.Internal, "failed to attach GPU: %v", err)
				}
			}

			next = next.With(InheritFilesForRestore, AddMountsForRestore)

			// Import GPU CRIU callbacks
			opts.CRIUCallback.Include(gpus.CRIUCallback(id))

			ctx = context.WithValue(ctx, keys.GPU_ID_CONTEXT_KEY, id)

			code, err = next(ctx, opts, resp, req)
			if err != nil {
				return nil, err
			}

			pid <- resp.PID

			log.Info().Uint32("PID", resp.PID).Str("controller", id).Msg("GPU support restored for process")

			return code, nil
		}
	}
}

func InheritFilesForRestore(next types.Restore) types.Restore {
	return func(ctx context.Context, opts types.Opts, resp *daemon.RestoreResp, req *daemon.RestoreReq) (code func() <-chan int, err error) {
		state := resp.GetState()
		if state == nil {
			return nil, status.Errorf(codes.InvalidArgument, "missing state. it should have been set by the adapter")
		}

		if req.Criu == nil {
			req.Criu = &criu.CriuOpts{}
		}

		key := strings.TrimPrefix(fmt.Sprintf(CONTROLLER_SHM_FILE_FORMATTER, state.GPUID), "/")
		fd := int32(3 + len(opts.ExtraFiles))

		if _, ok := opts.InheritFdMap[key]; ok {
			return nil, status.Errorf(codes.FailedPrecondition, "controller shm file already inherited")
		}
		opts.InheritFdMap[key] = fd

		id, ok := ctx.Value(keys.GPU_ID_CONTEXT_KEY).(string)
		if ok {
			// Open new GPU shm file
			shmFile, err := os.OpenFile(fmt.Sprintf(CONTROLLER_SHM_FILE_FORMATTER, id), os.O_RDWR, 0o644)
			if err != nil {
				return nil, status.Errorf(codes.Internal, "failed to open GPU shm file: %v", err)
			}
			defer shmFile.Close()

			opts.ExtraFiles = append(opts.ExtraFiles, shmFile)
			req.Criu.InheritFd = append(req.Criu.InheritFd, &criu.InheritFd{
				Fd:  proto.Int32(fd),
				Key: proto.String(key),
			})
		}

		mounts := make(map[uint64]any)
		utils.WalkTree(state, "Mounts", "Children", func(m *daemon.Mount) bool {
			mounts[m.ID] = nil
			return true
		})

		var toClose []*os.File
		var logDir string

		// Inherit hostmem files as well, if any
		utils.WalkTree(state, "OpenFiles", "Children", func(file *daemon.File) bool {
			path := file.Path

			var newPath string
			var newFile *os.File
			var pid int

			isPipe := strings.HasPrefix(file.Path, "pipe")
			isSocket := strings.HasPrefix(file.Path, "socket")
			isAnon := strings.HasPrefix(file.Path, "anon_inode")
			_, internal := mounts[file.MountID]

			external := !(internal || isPipe || isSocket || isAnon) // sockets and pipes are always in external mounts

			hostmemRegex := regexp.MustCompile(CONTROLLER_HOSTMEM_FILE_PATTERN)
			interceptorLogRegex := regexp.MustCompile(INTERCEPTOR_LOG_FILE_PATTERN)
			tracerLogRegex := regexp.MustCompile(TRACER_LOG_FILE_PATTERN)

			if matches := hostmemRegex.FindStringSubmatch(path); id != "" && len(matches) == 3 {
				pid, err = strconv.Atoi(matches[2])
				if err != nil {
					err = status.Errorf(codes.Internal, "failed to parse PID from hostmem file path %s: %v", path, err)
					return false
				}
				newPath = fmt.Sprintf(CONTROLLER_HOSTMEM_FILE_FORMATTER, id, pid)

				// Find the checkpoint hostmem file by matching the hostmem name in metadata
				// Checkpoint file format: size(8) + baseAddr(8) + segName(128) + data
				expectedSegName := fmt.Sprintf("hostmem-%d", pid)
				var hostMemSegSize uint64
				found := false

				// Try to find matching checkpoint file (workerIndex=0, contextIndex 0-9)
				matches, err := afero.Glob(opts.DumpFs, "gpu-hostmem-*")
				if err == nil {
					for _, filename := range matches {
						tempFile, openErr := opts.DumpFs.Open(filename)
						if openErr != nil {
							continue
						}

						// Read metadata: size(8) + baseAddr(8) + segName(128)
						metadataBuffer := make([]byte, 144) // 8 + 8 + 128
						_, readErr := tempFile.Read(metadataBuffer)
						tempFile.Close()
						if readErr != nil && readErr.Error() != "EOF" {
							continue
						}

						// Extract segName from bytes 16-144
						segNameBytes := metadataBuffer[16:144]
						// Find null terminator
						nullIdx := bytes.IndexByte(segNameBytes, 0)
						var segName string
						if nullIdx >= 0 {
							segName = string(segNameBytes[:nullIdx])
						} else {
							segName = string(segNameBytes)
						}

						// strip the leading `/`
						segName = strings.TrimSpace(segName)
						segName = strings.TrimPrefix(segName, "/")

						log.Debug().Str("file", filename).Str("segName", segName).Str("expected", expectedSegName).Msg("checking hostmem checkpoint file")

						if segName == expectedSegName {
							// Found the right file, extract size from first 8 bytes
							hostMemSegSize = binary.LittleEndian.Uint64(metadataBuffer[0:8])
							found = true
							log.Debug().Str("file", filename).Str("segName", segName).Uint64("size", hostMemSegSize).Msg("found matching hostmem checkpoint file")
							break
						}
					}
				}

				if !found {
					err = status.Errorf(codes.Internal, "failed to find checkpoint hostmem file for %s (expected segName: %s)", path, expectedSegName)
					return false
				}

				// Create hostmem file in host path (not using inherit FD)
				hostmemFile, createErr := os.OpenFile(newPath, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o666)
				if createErr != nil {
					err = status.Errorf(codes.Internal, "failed to create hostmem file %s: %v", newPath, createErr)
					return false
				}

				defer hostmemFile.Close()

				if err = hostmemFile.Chmod(0o666); err != nil {
					hostmemFile.Close()
					err = status.Errorf(codes.Internal, "failed to chmod hostmem file %s: %v", newPath, err)
					return false
				}

				// Truncate to the correct size
				if hostMemSegSize > 0 {
					truncErr := hostmemFile.Truncate(int64(hostMemSegSize))
					if truncErr != nil {
						hostmemFile.Close()
						err = status.Errorf(codes.Internal, "failed to truncate hostmem file %s to size %d: %v", newPath, hostMemSegSize, truncErr)
						return false
					}
					log.Debug().Str("path", newPath).Uint64("size", hostMemSegSize).Msg("created and truncated hostmem file in host path")
				} else {
					log.Debug().Str("path", newPath).Msg("created hostmem file in host path (no size info)")
				}

				return true // Don't inherit FD for hostmem, just create the file
			} else if matches := interceptorLogRegex.FindStringSubmatch(path); len(matches) == 4 {
				oldId := matches[2]
				pid, err = strconv.Atoi(matches[3])
				if err != nil {
					err = status.Errorf(codes.Internal, "failed to parse PID from interceptor log file path %s: %v", path, err)
					return false
				}
				if id == "" {
					id = oldId
				}
				logDir, err = EnsureLogDir(id, req.UID, req.GID)
				if err != nil {
					err = status.Errorf(codes.Internal, "failed to recreate log directory %s: %v", logDir, err)
					return false
				}
				newPath = fmt.Sprintf(INTERCEPTOR_LOG_FILE_FORMATTER, config.Global.GPU.LogDir, id, pid)
			} else if matches := tracerLogRegex.FindStringSubmatch(path); len(matches) == 4 {
				oldId := matches[2]
				pid, err = strconv.Atoi(matches[3])
				if err != nil {
					err = status.Errorf(codes.Internal, "failed to parse PID from tracer log file path %s: %v", path, err)
					return false
				}
				if id == "" {
					id = oldId
				}
				logDir, err = EnsureLogDir(id, req.UID, req.GID)
				if err != nil {
					err = status.Errorf(codes.Internal, "failed to recreate log directory %s: %v", logDir, err)
					return false
				}
				newPath = fmt.Sprintf(TRACER_LOG_FILE_FORMATTER, config.Global.GPU.LogDir, id, pid)
			} else {
				return true // not a file we care about
			}

			newFile, err = os.OpenFile(newPath, os.O_RDWR|os.O_CREATE, 0o644)
			if err != nil {
				err = status.Errorf(codes.Internal, "failed to reopen file for inheriting FD %s: %v", newPath, err)
				return false
			}

			if external {
				key = fmt.Sprintf("file[%x:%x]", file.MountID, file.Inode)
			} else {
				key = strings.TrimPrefix(path, "/")
			}
			fd = int32(3 + len(opts.ExtraFiles))

			if _, ok := opts.InheritFdMap[key]; ok {
				return true // already inherited
			}

			log.Debug().Str("key", key).Int32("fd", fd).Bool("external", external).Str("old", path).Str("new", newPath).Msgf("inheriting file")

			opts.InheritFdMap[key] = fd
			opts.ExtraFiles = append(opts.ExtraFiles, newFile)
			toClose = append(toClose, newFile)
			req.Criu.InheritFd = append(req.Criu.InheritFd, &criu.InheritFd{
				Key: proto.String(key),
				Fd:  proto.Int32(fd),
			})

			return true
		})

		for _, file := range toClose {
			defer file.Close()
		}

		if err != nil {
			return nil, err
		}

		ctx = context.WithValue(ctx, keys.GPU_LOG_DIR_CONTEXT_KEY, logDir)

		return next(ctx, opts, resp, req)
	}
}

///////////////////////////////////////
//// Interception/Tracing Adapters ////
///////////////////////////////////////

// Adapter that restore GPU interception to the request based on the job type.
// Each plugin must implement its own support for restoring GPU interception.
func InterceptionRestore(next types.Restore) types.Restore {
	return func(ctx context.Context, opts types.Opts, resp *daemon.RestoreResp, req *daemon.RestoreReq) (code func() <-chan int, err error) {
		id, ok := ctx.Value(keys.GPU_ID_CONTEXT_KEY).(string)
		if !ok {
			return nil, status.Errorf(codes.Internal, "failed to get GPU ID from context")
		}

		plugin := opts.Plugins.Get("gpu")
		if !plugin.IsInstalled() {
			return nil, status.Errorf(codes.FailedPrecondition, "Please install the GPU plugin for GPU C/R support")
		}

		logDir, err := EnsureLogDir(id, req.UID, req.GID)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to ensure GPU log dir: %v", err)
		}

		ctx = context.WithValue(ctx, keys.GPU_LOG_DIR_CONTEXT_KEY, logDir)

		t := req.GetType()
		switch t {
		case "process":
			// Nothing to do
		default:
			// Use plugin-specific handler, if available
			features.GPUInterceptionRestore.IfAvailable(func(
				name string,
				pluginInterception types.Adapter[types.Restore],
			) error {
				next = next.With(pluginInterception)
				return nil
			}, t)
		}

		log.Info().Str("plugin", "gpu").Str("ID", id).Str("type", t).Msg("restoring GPU interception")

		return next(ctx, opts, resp, req)
	}
}

// Adapter that tells CRIU about the external GPU mounts.
func AddMountsForRestore(next types.Restore) types.Restore {
	return func(ctx context.Context, opts types.Opts, resp *daemon.RestoreResp, req *daemon.RestoreReq) (code func() <-chan int, err error) {
		state := resp.GetState()
		if state == nil {
			return nil, status.Errorf(
				codes.InvalidArgument,
				"missing state. at least PID is required in resp.state",
			)
		}

		utils.WalkTree(state, "Mounts", "Children", func(m *daemon.Mount) bool {
			if NVIDIA_MOUNTS_PATTERN.MatchString(m.Root) {
				log.Debug().Str("root", m.Root).Str("mount_path", m.MountPoint).Msg("marking NVIDIA GPU mount as external")
				req.Criu.External = append(req.Criu.External, fmt.Sprintf("mnt[%s]:%s", m.MountPoint, m.Root))
			}
			return true
		})

		return next(ctx, opts, resp, req)
	}
}

// Adapter that restores GPU tracing to the request based on the job type.
// Each plugin must implement its own support for restoring GPU tracing.
func TracingRestore(next types.Restore) types.Restore {
	return func(ctx context.Context, opts types.Opts, resp *daemon.RestoreResp, req *daemon.RestoreReq) (code func() <-chan int, err error) {
		id, ok := ctx.Value(keys.GPU_ID_CONTEXT_KEY).(string) // Try to use existing ID
		if !ok {
			id = uuid.New().String()
			ctx = context.WithValue(ctx, keys.GPU_ID_CONTEXT_KEY, id)
		}

		plugin := opts.Plugins.Get("gpu/tracer")
		if !plugin.IsInstalled() {
			return nil, status.Errorf(codes.FailedPrecondition, "Please install the GPU/tracer plugin for GPU tracing support")
		}

		logDir, err := EnsureLogDir(id, req.UID, req.GID)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to ensure GPU log dir: %v", err)
		}

		ctx = context.WithValue(ctx, keys.GPU_LOG_DIR_CONTEXT_KEY, logDir)

		t := req.GetType()
		switch t {
		case "process":
			// Nothing to do
		default:
			// Use plugin-specific handler
			features.GPUTracingRestore.IfAvailable(func(
				name string,
				pluginTracing types.Adapter[types.Restore],
			) error {
				next = next.With(pluginTracing)
				return nil
			}, t)
		}

		log.Info().Str("plugin", "gpu/tracer").Str("ID", id).Str("type", t).Msg("restoring GPU tracing")

		return next(ctx, opts, resp, req)
	}
}
