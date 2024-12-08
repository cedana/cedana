package process

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	criu_proto "buf.build/gen/go/cedana/criu/protocolbuffers/go/criu"
	"github.com/cedana/cedana/pkg/criu"
	"github.com/cedana/cedana/pkg/types"
	"github.com/cedana/cedana/pkg/utils"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

const STATE_FILE = "process_state.json"

// Sets the PID from the request to the process state
// if not already set before.
func SetPIDForDump(next types.Dump) types.Dump {
	return func(ctx context.Context, server types.ServerOpts, nfy *criu.NotifyCallbackMulti, resp *daemon.DumpResp, req *daemon.DumpReq) (exited chan int, err error) {
		if resp.GetState() == nil {
			resp.State = &daemon.ProcessState{}
		}

		if resp.GetState().GetPID() == 0 {
			pid := req.GetDetails().GetProcess().GetPID()
			if pid == 0 {
				return nil, status.Errorf(codes.InvalidArgument, "missing PID")
			}
			resp.State.PID = pid
		}

		return next(ctx, server, nfy, resp, req)
	}
}

// Fills process state in the dump response.
// Requires at least the PID to be present in the DumpResp.State
// Also saves the state to a file in the dump directory, post dump.
func FillProcessStateForDump(next types.Dump) types.Dump {
	return func(ctx context.Context, server types.ServerOpts, nfy *criu.NotifyCallbackMulti, resp *daemon.DumpResp, req *daemon.DumpReq) (exited chan int, err error) {
		state := resp.GetState()
		if state == nil {
			return nil, status.Errorf(
				codes.InvalidArgument,
				"missing state. at least PID is required in resp.state",
			)
		}

		if state.PID == 0 {
			return nil, status.Errorf(
				codes.NotFound,
				"missing PID. Ensure an adapter sets this PID in response.",
			)
		}

		err = utils.FillProcessState(ctx, state.PID, state)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to fill process state: %v", err)
		}

		exited, err = next(ctx, server, nfy, resp, req)
		if err != nil {
			return exited, err
		}

		// Post dump, save the state to a file in the dump
		stateFile := filepath.Join(req.GetCriu().GetImagesDir(), STATE_FILE)
		if err := utils.SaveJSONToFile(state, stateFile); err != nil {
			log.Warn().Err(err).Str("file", stateFile).Msg("failed to save process state")
		}

		return exited, nil
	}
}

// Detect and sets shell job option for CRIU
func DetectShellJobForDump(next types.Dump) types.Dump {
	return func(ctx context.Context, server types.ServerOpts, nfy *criu.NotifyCallbackMulti, resp *daemon.DumpResp, req *daemon.DumpReq) (exited chan int, err error) {
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

		return next(ctx, server, nfy, resp, req)
	}
}

// Close common file descriptors b/w the parent and child process
func CloseCommonFilesForDump(next types.Dump) types.Dump {
	return func(ctx context.Context, server types.ServerOpts, nfy *criu.NotifyCallbackMulti, resp *daemon.DumpResp, req *daemon.DumpReq) (exited chan int, err error) {
		pid := resp.GetState().GetPID()
		if pid == 0 {
			return nil, status.Errorf(
				codes.NotFound,
				"missing PID. Ensure an adapter sets this PID in response before.",
			)
		}

		err = utils.CloseCommonFds(ctx, int32(os.Getpid()), int32(pid))
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to close common fds: %v", err)
		}

		return next(ctx, server, nfy, resp, req)
	}
}
