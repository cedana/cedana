package process

import (
	"context"
	"fmt"
	"strings"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	criu_proto "buf.build/gen/go/cedana/criu/protocolbuffers/go/criu"
	"github.com/cedana/cedana/pkg/types"
	"github.com/cedana/cedana/pkg/utils"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// Reload process state from the dump dir in the restore response
func ReloadProcessStateForRestore(next types.Restore) types.Restore {
	return func(ctx context.Context, opts types.Opts, resp *daemon.RestoreResp, req *daemon.RestoreReq) (chan int, error) {
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

		exited, err := next(ctx, opts, resp, req)
		if err != nil {
			return exited, err
		}

		return exited, err
	}
}

// Detect and sets shell job option for CRIU
func DetectShellJobForRestore(next types.Restore) types.Restore {
	return func(ctx context.Context, opts types.Opts, resp *daemon.RestoreResp, req *daemon.RestoreReq) (chan int, error) {
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
	return func(ctx context.Context, opts types.Opts, resp *daemon.RestoreResp, req *daemon.RestoreReq) (chan int, error) {
		state := resp.GetState()
		if state == nil {
			log.Warn().Msg("no process info found. it should have been filled by an adapter")
			return next(ctx, opts, resp, req)
		}

		// Set the inherited fds
		if req.Criu == nil {
			req.Criu = &criu_proto.CriuOpts{}
		}
		inheritFds := req.Criu.InheritFd

		files := state.GetOpenFiles()
		mounts := state.GetMounts()

		mountIds := make(map[uint64]any)
		for _, m := range mounts {
			mountIds[m.ID] = nil
		}

		for _, f := range files {
			isPipe := strings.HasPrefix(f.Path, "pipe")
			isSocket := strings.HasPrefix(f.Path, "socket")
			isAnon := strings.HasPrefix(f.Path, "anon_inode")
			_, internal := mountIds[f.MountID]

			external := !(internal || isPipe || isSocket || isAnon) // sockets and pipes are always in external mounts

			if external {
				inheritFds = append(inheritFds, &criu_proto.InheritFd{
					Fd:  proto.Int32(int32(f.Fd)),
					Key: proto.String(fmt.Sprintf("file[%x:%x]", f.MountID, f.Inode)),
				})
				log.Warn().Msgf("inherited external file %s with fd %d. assuming it still exists", f.Path, f.Fd)
			} else {
				if f.IsTTY {
					return nil, status.Errorf(codes.FailedPrecondition,
						"found open STDIN file %s with fd %d which is a TTY and so restoring will fail because no TTY to inherit", f.Path, f.Fd)
				}
				f.Path = strings.TrimPrefix(f.Path, "/")
				if f.Fd == 0 {
					if req.Attachable {
						inheritFds = append(inheritFds, &criu_proto.InheritFd{
							Fd:  proto.Int32(int32(f.Fd)),
							Key: proto.String(f.Path),
						})
					} else {
						log.Warn().Msgf("found open non-TTY STDIN file %s with fd %d and req.Attachable is not set so assuming it still exists", f.Path, f.Fd)
					}
				} else if f.Fd == 1 || f.Fd == 2 {
					if req.Attachable || req.Log != "" {
						inheritFds = append(inheritFds, &criu_proto.InheritFd{
							Fd:  proto.Int32(int32(f.Fd)),
							Key: proto.String(f.Path),
						})
					} else {
						log.Warn().Msgf("found open non-TTY STDOUT/STDERR file %s with fd %d and req.Log/Attachable is not set so assuming it still exists", f.Path, f.Fd)
					}
				}
			}
		}

		req.Criu.InheritFd = inheritFds

		return next(ctx, opts, resp, req)
	}
}
