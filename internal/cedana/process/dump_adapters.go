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

const STATE_FILE = "process_state.json"

// Sets the PID from the request to the process state
// if not already set before.
func SetPIDForDump(next types.Dump) types.Dump {
	return func(ctx context.Context, opts types.Opts, resp *daemon.DumpResp, req *daemon.DumpReq) (code func() <-chan int, err error) {
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

		return next(ctx, opts, resp, req)
	}
}

// Fills process state in the dump response.
func FillProcessStateForDump(next types.Dump) types.Dump {
	return func(ctx context.Context, opts types.Opts, resp *daemon.DumpResp, req *daemon.DumpReq) (code func() <-chan int, err error) {
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

		err = utils.FillProcessState(ctx, state.PID, state, true)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to fill process state: %v", err)
		}

		return next(ctx, opts, resp, req)
	}
}

// Saves process state in the dump directory.
func SaveProcessStateForDump(next types.Dump) types.Dump {
	return func(ctx context.Context, opts types.Opts, resp *daemon.DumpResp, req *daemon.DumpReq) (code func() <-chan int, err error) {
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

		// Save the state to a file in the dump
		if err := utils.SaveJSONToFile(state, STATE_FILE, opts.DumpFs); err != nil {
			log.Warn().Err(err).Str("file", STATE_FILE).Msg("failed to save process state")
		}

		return next(ctx, opts, resp, req)
	}
}

// Detect and sets shell job option for CRIU
func DetectShellJobForDump(next types.Dump) types.Dump {
	return func(ctx context.Context, opts types.Opts, resp *daemon.DumpResp, req *daemon.DumpReq) (code func() <-chan int, err error) {
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

// Detects if the process is using IOUring and sets appropriate options for CRIU
// XXX: Currently IO uring C/R is not supported by CRIU, so we return an error.
// ref: https://criu.org/Google_Summer_of_Code_Ideas#IOUring_support
func DetectIOUringForDump(next types.Dump) types.Dump {
	return func(ctx context.Context, opts types.Opts, resp *daemon.DumpResp, req *daemon.DumpReq) (code func() <-chan int, err error) {
		state := resp.GetState()
		if state == nil {
			log.Warn().Msg("no process info found. it should have been filled by an adapter")
			return next(ctx, opts, resp, req)
		}

		var ioUring bool

		utils.WalkTree(state, "OpenFiles", "Children", func(f *daemon.File) bool {
			if strings.Contains(f.Path, "io_uring") {
				ioUring = true
			}
			return !ioUring
		})

		if ioUring {
			return nil, status.Errorf(codes.Unimplemented, "IOUring dump is not supported at the moment")
		}

		return next(ctx, opts, resp, req)
	}
}

// Detects any open files that are in an external namespace
// and sets appropriate options for CRIU.
// Also detects any TTY files and sets options for CRIU.
func AddExternalFilesForDump(next types.Dump) types.Dump {
	return func(ctx context.Context, opts types.Opts, resp *daemon.DumpResp, req *daemon.DumpReq) (code func() <-chan int, err error) {
		state := resp.GetState()
		if state == nil {
			log.Warn().Msg("no process info found. it should have been filled by an adapter")
			return next(ctx, opts, resp, req)
		}

		internalMounts := make(map[uint64]any)
		utils.WalkTree(state, "Mounts", "Children", func(m *daemon.Mount) bool {
			internalMounts[m.ID] = nil
			return true
		})

		if req.GetCriu() == nil {
			req.Criu = &criu_proto.CriuOpts{}
		}

		utils.WalkTree(state, "OpenFiles", "Children", func(f *daemon.File) bool {
			isPipe := strings.HasPrefix(f.Path, "pipe")
			isSocket := strings.HasPrefix(f.Path, "socket")
			isAnon := strings.HasPrefix(f.Path, "anon_inode")
			_, internal := internalMounts[f.MountID]

			external := !(internal || isPipe || isSocket || isAnon) // sockets and pipes are always in external mounts

			if external {
				if f.IsTTY {
					log.Debug().Str("path", f.Path).Uint64("rdev", f.Rdev).Uint64("dev", f.Dev).Msg("marking TTY file as external")
					req.Criu.External = append(req.Criu.External, fmt.Sprintf("tty[%x:%x]", f.Rdev, f.Dev))
				} else {
					log.Debug().Str("path", f.Path).Uint64("mount_id", f.MountID).Uint64("inode", f.Inode).Msg("marking file as external")
					req.Criu.External = append(req.Criu.External, fmt.Sprintf("file[%x:%x]", f.MountID, f.Inode))
				}
			}

			return true
		})

		return next(ctx, opts, resp, req)
	}
}
