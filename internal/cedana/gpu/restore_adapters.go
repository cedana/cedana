package gpu

import (
	"context"
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
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// Adapter that restores GPU support to the request.
func Restore(gpus Manager) types.Adapter[types.Restore] {
	return func(next types.Restore) types.Restore {
		return func(ctx context.Context, opts types.Opts, resp *daemon.RestoreResp, req *daemon.RestoreReq) (code func() <-chan int, err error) {
			next = next.With(InheritFilesForRestore)

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

			_, end := profiling.StartTimingCategory(ctx, "gpu", gpus.Attach)
			id, err := gpus.Attach(ctx, pid)
			end()
			if err != nil {
				return nil, status.Errorf(codes.Internal, "failed to attach GPU: %v", err)
			}

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

		extraFiles, _ := ctx.Value(keys.EXTRA_FILES_CONTEXT_KEY).([]*os.File)
		inheritFdMap, _ := ctx.Value(keys.INHERIT_FD_MAP_CONTEXT_KEY).(map[string]int32)

		if req.Criu == nil {
			req.Criu = &criu.CriuOpts{}
		}

		key := strings.TrimPrefix(fmt.Sprintf(CONTROLLER_SHM_FILE_FORMATTER, state.GPUID), "/")
		fd := int32(3 + len(extraFiles))

		if _, ok := inheritFdMap[key]; ok {
			return nil, status.Errorf(codes.FailedPrecondition, "controller shm file already inherited")
		}
		inheritFdMap[key] = fd

		id, ok := ctx.Value(keys.GPU_ID_CONTEXT_KEY).(string)
		if ok {
			// Open new GPU shm file
			shmFile, err := os.OpenFile(fmt.Sprintf(CONTROLLER_SHM_FILE_FORMATTER, id), os.O_RDWR, 0o644)
			if err != nil {
				return nil, status.Errorf(codes.Internal, "failed to open GPU shm file: %v", err)
			}
			defer shmFile.Close()

			extraFiles = append(extraFiles, shmFile)
			req.Criu.InheritFd = append(req.Criu.InheritFd, &criu.InheritFd{
				Fd:  proto.Int32(fd),
				Key: proto.String(key),
			})
		}

		var toClose []*os.File
		var logDir string

		// Inherit hostmem files as well, if any
		utils.WalkTree(state, "OpenFiles", "Children", func(file *daemon.File) bool {
			path := file.Path

			var newPath string
			var newFile *os.File
			var pid int

			hostmemRegex := regexp.MustCompile(CONTROLLER_HOSTMEM_FILE_PATTERN)
			interceptorLogRegex := regexp.MustCompile(fmt.Sprintf(INTERCEPTOR_LOG_FILE_PATTERN, config.Global.GPU.LogDir))
			tracerLogRegex := regexp.MustCompile(fmt.Sprintf(TRACER_LOG_FILE_PATTERN, config.Global.GPU.LogDir))

			if matches := hostmemRegex.FindStringSubmatch(path); id != "" && len(matches) == 3 {
				pid, err = strconv.Atoi(matches[2])
				if err != nil {
					err = status.Errorf(codes.Internal, "failed to parse PID from hostmem file path %s: %v", path, err)
					return false
				}
				newPath = fmt.Sprintf(CONTROLLER_HOSTMEM_FILE_FORMATTER, id, pid)
			} else if matches := interceptorLogRegex.FindStringSubmatch(path); len(matches) == 3 {
				oldId := matches[1]
				pid, err = strconv.Atoi(matches[2])
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
			} else if matches := tracerLogRegex.FindStringSubmatch(path); len(matches) == 3 {
				oldId := matches[1]
				pid, err = strconv.Atoi(matches[2])
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

			key = strings.TrimPrefix(path, "/")
			fd = int32(3 + len(extraFiles))

			if _, ok := inheritFdMap[key]; ok {
				return true // already inherited
			}
			inheritFdMap[key] = fd
			extraFiles = append(extraFiles, newFile)
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

		ctx = context.WithValue(ctx, keys.EXTRA_FILES_CONTEXT_KEY, extraFiles)
		ctx = context.WithValue(ctx, keys.INHERIT_FD_MAP_CONTEXT_KEY, inheritFdMap)
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
