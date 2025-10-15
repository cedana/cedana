package process

import (
	"context"
	"fmt"
	"os"
	"strings"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	criu_proto "buf.build/gen/go/cedana/criu/protocolbuffers/go/criu"

	"github.com/cedana/cedana/pkg/keys"
	"github.com/cedana/cedana/pkg/types"
	"github.com/cedana/cedana/pkg/utils"

	"github.com/rs/zerolog/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// Adapter that writes PID to a file after the next handler is called.
func WritePIDFileForRestore(next types.Restore) types.Restore {
	return func(ctx context.Context, opts types.Opts, resp *daemon.RestoreResp, req *daemon.RestoreReq) (code func() <-chan int, err error) {
		code, err = next(ctx, opts, resp, req)
		if err != nil {
			return code, err
		}

		pidFile := req.PidFile
		if pidFile == "" {
			return code, err
		}

		file, err := os.Create(pidFile)
		if err != nil {
			log.Warn().Err(err).Msgf("Failed to create PID file %s", pidFile)
			resp.Messages = append(resp.Messages, fmt.Sprintf("Failed to create PID file %s: %s", pidFile, err.Error()))
		}

		_, err = fmt.Fprintf(file, "%d", resp.PID)
		if err != nil {
			log.Warn().Err(err).Msgf("Failed to write PID to file %s", pidFile)
			resp.Messages = append(resp.Messages, fmt.Sprintf("Failed to write PID to file %s: %s", pidFile, err.Error()))
		}

		log.Debug().Msgf("Wrote PID %d to file %s", resp.PID, pidFile)

		// Do not fail the request if we cannot write the PID file

		return code, nil
	}
}

// Reload process state from the dump dir in the restore response
func ReloadProcessStateForRestore(next types.Restore) types.Restore {
	return func(ctx context.Context, opts types.Opts, resp *daemon.RestoreResp, req *daemon.RestoreReq) (code func() <-chan int, err error) {
		// Check if path is a directory
		path := req.GetCriu().GetImagesDir()
		if path == "" {
			return nil, status.Errorf(
				codes.InvalidArgument,
				"missing path. should have been set by an adapter",
			)
		}

		state := &daemon.ProcessState{}

		if err := utils.LoadJSONFromFile(STATE_FILE, state, opts.DumpFs); err != nil {
			return nil, status.Errorf(
				codes.Internal,
				"failed to load process state from dump: %v",
				err,
			)
		}

		resp.State = state

		return next(ctx, opts, resp, req)
	}
}

// Detect and sets shell job option for CRIU
func DetectShellJobForRestore(next types.Restore) types.Restore {
	return func(ctx context.Context, opts types.Opts, resp *daemon.RestoreResp, req *daemon.RestoreReq) (code func() <-chan int, err error) {
		var isShellJob bool
		if state := resp.GetState(); state != nil {
			if state.SID != state.PID {
				isShellJob = true
			}
		} else {
			log.Warn().Msg("No process info found. it should have been filled by an adapter")
		}

		if req.GetCriu() == nil {
			req.Criu = &criu_proto.CriuOpts{}
		}

		req.Criu.ShellJob = proto.Bool(isShellJob)

		return next(ctx, opts, resp, req)
	}
}

// If req.Attachable is set, inherit fd is set to 0, 1, 2.
// assuming CRIU will be spawned with these set to appropriate files later on.
// If just req.Log is set, inherit fd is set only for 1, 2. stdin is not inherited.
// If these options are not set, it is assumed that these files still exist
// and the restore will just fail if they don't.
//
// If a file is a TTY, restore will fail because there is no TTY to inherit.
//
// If there were any external (namespace) files during dump, they are also
// added to be inherited. Note that this would still fail if the files don't exist.
func InheritFilesForRestore(next types.Restore) types.Restore {
	return func(ctx context.Context, opts types.Opts, resp *daemon.RestoreResp, req *daemon.RestoreReq) (code func() <-chan int, err error) {
		state := resp.GetState()
		if state == nil {
			log.Warn().Msg("no process info found. it should have been filled by an adapter")
			return next(ctx, opts, resp, req)
		}

		daemonless, _ := ctx.Value(keys.DAEMONLESS_CONTEXT_KEY).(bool)
		visitedStdioFds := make(map[uint64]bool)
		inheritFdMap, _ := ctx.Value(keys.INHERIT_FD_MAP_CONTEXT_KEY).(map[string]int32)
		extraFiles, _ := ctx.Value(keys.EXTRA_FILES_CONTEXT_KEY).([]*os.File)

		// Set the inherited fds
		if req.Criu == nil {
			req.Criu = &criu_proto.CriuOpts{}
		}
		inheritFds := req.Criu.InheritFd

		mountIds := make(map[uint64]any)
		utils.WalkTree(state, "Mounts", "Children", func(m *daemon.Mount) bool {
			mountIds[m.ID] = nil
			return true
		})

		utils.WalkTree(state, "OpenFiles", "Children", func(f *daemon.File) bool {
			isPipe := strings.HasPrefix(f.Path, "pipe")
			isSocket := strings.HasPrefix(f.Path, "socket")
			isAnon := strings.HasPrefix(f.Path, "anon_inode")
			_, internal := mountIds[f.MountID]

			var key string
			var fd int32
			var extraFile *os.File

			external := !(internal || isPipe || isSocket || isAnon) // sockets and pipes are always in external mounts

			if external {

				// Inherit all external namespace files, expecting them to still exist

				if f.IsTTY {
					key = fmt.Sprintf("tty[%x:%x]", f.Rdev, f.Dev)
					fd = int32(f.Fd)
				} else {
					key = fmt.Sprintf("file[%x:%x]", f.MountID, f.Inode)
					fd = int32(f.Fd)
				}
				log.Warn().Msgf("inherited external file %s with fd %d. assuming it still exists", f.Path, f.Fd)

			} else {

				// Inherit stdio files that are not external

				if visitedStdioFds[f.Fd] { // Stdio fds should only be inherited once
					return true
				}
				visitedStdioFds[f.Fd] = true

				if f.IsTTY || daemonless {
					if !daemonless {
						err = status.Errorf(codes.FailedPrecondition,
							"found open file %s with fd %d which is a TTY and so restoring will fail because no TTY to inherit. Try --no-server restore", f.Path, f.Fd)
						return false
					}

					switch f.Fd {
					case 0:
						extraFile = os.Stdin
						key = strings.TrimPrefix(f.Path, "/")
						fd = int32(3 + len(extraFiles))
					case 1:
						extraFile = os.Stdout
						key = strings.TrimPrefix(f.Path, "/")
						fd = int32(3 + len(extraFiles))
					case 2:
						extraFile = os.Stderr
						key = strings.TrimPrefix(f.Path, "/")
						fd = int32(3 + len(extraFiles))
					}
				} else if f.Fd == 0 {
					if req.Attachable {
						key = strings.TrimPrefix(f.Path, "/")
						fd = int32(f.Fd)
						log.Debug().Msgf("found open STDIN file %s with fd %d and req.Attachable is set so inheriting it", f.Path, f.Fd)
					} else {
						log.Warn().Msgf("found open non-TTY STDIN file %s with fd %d and req.Attachable is not set so assuming it still exists", f.Path, f.Fd)
					}
				} else if f.Fd == 1 || f.Fd == 2 {
					log.Debug().Msgf("found open STDOUT/STDERR file %s with fd %d and req.Log/Attachable is set so inheriting it", f.Path, f.Fd)
					if req.Attachable || req.Log != "" {
						key = strings.TrimPrefix(f.Path, "/")
						fd = int32(f.Fd)
					} else {
						log.Warn().Msgf("found open non-TTY STDOUT/STDERR file %s with fd %d and req.Log/Attachable is not set so assuming it still exists", f.Path, f.Fd)
					}
				}
			}

			if _, ok := inheritFdMap[key]; key == "" || ok {
				return true
			}
			inheritFdMap[key] = fd
			extraFiles = append(extraFiles, extraFile)
			inheritFds = append(inheritFds, &criu_proto.InheritFd{
				Fd:  proto.Int32(fd),
				Key: proto.String(key),
			})

			return true
		})

		if err != nil {
			return nil, err
		}

		req.Criu.InheritFd = inheritFds
		ctx = context.WithValue(ctx, keys.EXTRA_FILES_CONTEXT_KEY, extraFiles)
		ctx = context.WithValue(ctx, keys.INHERIT_FD_MAP_CONTEXT_KEY, inheritFdMap)

		return next(ctx, opts, resp, req)
	}
}
