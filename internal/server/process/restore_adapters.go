package process

import (
	"context"
	"path/filepath"
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

// Fill process state in the restore response
func FillProcessStateForRestore(next types.Restore) types.Restore {
	return func(ctx context.Context, server types.ServerOpts, resp *daemon.RestoreResp, req *daemon.RestoreReq) (chan int, error) {
		// Check if path is a directory
		path := req.GetCriu().GetImagesDir()
		if path == "" {
			return nil, status.Errorf(
				codes.InvalidArgument,
				"missing path. should have been set by an adapter",
			)
		}

		stateFile := filepath.Join(path, STATE_FILE)
		state := &daemon.ProcessState{}

		if err := utils.LoadJSONFromFile(stateFile, state); err != nil {
			return nil, status.Errorf(
				codes.Internal,
				"failed to load process state from dump: %v",
				err,
			)
		}

		resp.State = state

		exited, err := next(ctx, server, resp, req)
		if err != nil {
			return exited, err
		}

		// Try to update the process state with the latest information,
		// Only possible if process is still running, otherwise ignore.
		_ = utils.FillProcessState(ctx, state.PID, state)

		return exited, err
	}
}

// Detect and sets shell job option for CRIU
func DetectShellJobForRestore(next types.Restore) types.Restore {
	return func(ctx context.Context, server types.ServerOpts, resp *daemon.RestoreResp, req *daemon.RestoreReq) (chan int, error) {
		var isShellJob bool
		if info := resp.GetState().GetInfo(); info != nil {
			for _, f := range info.GetOpenFiles() {
				if strings.Contains(f.Path, "pts") {
					isShellJob = true
					break
				}
			}
		} else {
			log.Warn().Msg("No process info found. it should have been filled by an adapter")
		}

		if req.GetCriu() == nil {
			req.Criu = &criu_proto.CriuOpts{}
		}

		// Only set unless already set
		if req.GetCriu().ShellJob == nil {
			req.Criu.ShellJob = proto.Bool(isShellJob)
		}

		return next(ctx, server, resp, req)
	}
}

// Open stdio files from the dump state are inherited by the restored process.
// They are set to inherit the 0, 1, 2 file descriptors, assuming CRIU cmd
// will be launched with these set to appropriate files.
func InheritStdioForRestore(next types.Restore) types.Restore {
	return func(ctx context.Context, server types.ServerOpts, resp *daemon.RestoreResp, req *daemon.RestoreReq) (chan int, error) {
		inheritFds := req.GetCriu().GetInheritFd()

		if info := resp.GetState().GetInfo(); info != nil {
			for _, f := range info.GetOpenFiles() {
				f.Path = strings.TrimPrefix(f.Path, "/")
				if f.Fd == 0 || f.Fd == 1 || f.Fd == 2 {
					inheritFds = append(inheritFds, &criu_proto.InheritFd{
						Fd:  proto.Int32(int32(f.Fd)),
						Key: proto.String(f.Path),
					})
				} else {
					log.Warn().Msgf("found non-stdio open file %s with fd %d", f.Path, f.Fd)
				}
			}
		} else {
			log.Warn().Msg("no process info found. it should have been filled by an adapter")
		}

		// Set the inherited fds
		if req.GetCriu() == nil {
			req.Criu = &criu_proto.CriuOpts{}
		}
		req.GetCriu().InheritFd = inheritFds

		return next(ctx, server, resp, req)
	}
}
