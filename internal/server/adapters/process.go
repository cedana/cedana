package adapters

// Defines all the adapters that manage the process-level details

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/cedana/cedana/pkg/api/daemon"
	"github.com/cedana/cedana/pkg/types"
	"github.com/cedana/cedana/pkg/utils"
	"github.com/rs/zerolog/log"
	"github.com/shirou/gopsutil/v4/process"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	STATE_FILE                         = "process_state.json"
	RESTORE_OUTPUT_FILE_PATH_FORMATTER = "/var/log/cedana-restore-%d.log"
)

////////////////////////
//// Dump Adapters /////
////////////////////////

// Check if the process exists, and is running
func CheckProcessExistsForDump(h types.DumpHandler) types.DumpHandler {
	return func(ctx context.Context, wg *sync.WaitGroup, resp *daemon.DumpResp, req *daemon.DumpReq) error {
		pid := req.GetPID()
		if pid == 0 {
			return status.Errorf(codes.InvalidArgument, "missing PID")
		}
		exists, err := process.PidExists(int32(pid))
		if err != nil {
			return status.Errorf(codes.Internal, "failed to check process: %v", err)
		}
		if !exists {
			return status.Errorf(codes.NotFound, "process not found: %d", pid)
		}

		if resp.GetState() == nil {
			resp.State = &daemon.ProcessState{}
		}

		resp.State.PID = uint32(pid)

		return h(ctx, wg, resp, req)
	}
}

// Fills process state in the dump response.
// Requires at least the PID to be present in the DumpResp.State
// Also saves the state to a file in the dump directory, post dump.
func FillProcessStateForDump(h types.DumpHandler) types.DumpHandler {
	return func(ctx context.Context, wg *sync.WaitGroup, resp *daemon.DumpResp, req *daemon.DumpReq) error {
		state := resp.GetState()
		if state == nil {
			return status.Errorf(codes.InvalidArgument, "missing state. at least PID is required in resp.state")
		}

		if state.PID == 0 {
			return status.Errorf(codes.NotFound, "missing PID. Ensure an adapter sets this PID in response.")
		}

		err := utils.FillProcessState(ctx, state.PID, state)
		if err != nil {
			return status.Errorf(codes.Internal, "failed to fill process state: %v", err)
		}

		err = h(ctx, wg, resp, req)
		if err != nil {
			return err
		}

		// Post dump, save the state to a file in the dump
		stateFile := filepath.Join(req.GetCriu().GetImagesDir(), STATE_FILE)
		if err := utils.SaveJSONToFile(state, stateFile); err != nil {
			log.Warn().Err(err).Str("file", stateFile).Msg("failed to save process state")
		}

		return nil
	}
}

// Detect and sets network options for CRIU
// XXX YA: Enforces unsuitable options for CRIU. Some times, we may
// not want to use TCP established connections. Also, for external unix
// sockets, the flag is deprecated. The correct way is to use the
// --external flag in CRIU.
func DetectNetworkOptionsForDump(h types.DumpHandler) types.DumpHandler {
	return func(ctx context.Context, wg *sync.WaitGroup, resp *daemon.DumpResp, req *daemon.DumpReq) error {
		var hasTCP, hasExtUnixSocket bool

		if state := resp.GetState(); state != nil {
			for _, Conn := range state.GetInfo().GetOpenConnections() {
				if Conn.Type == syscall.SOCK_STREAM { // TCP
					hasTCP = true
				}
				if Conn.Type == syscall.AF_UNIX { // Interprocess
					hasExtUnixSocket = true
				}
			}

			activeTCP, err := utils.HasActiveTCPConnections(int32(state.GetPID()))
			if err != nil {
				return status.Errorf(codes.Internal, "failed to check active TCP connections: %v", err)
			}
			hasTCP = hasTCP || activeTCP
		} else {
			log.Warn().Msg("No process info found. it should have been filled by an adapter")
		}

		// Set the network options
		if req.GetCriu() == nil {
			req.Criu = &daemon.CriuOpts{}
		}

		req.Criu.TcpEstablished = hasTCP || req.GetCriu().GetTcpEstablished()
		req.Criu.ExtUnixSk = hasExtUnixSocket || req.GetCriu().GetExtUnixSk()

		return h(ctx, wg, resp, req)
	}
}

// Detect and sets shell job option for CRIU
func DetectShellJobForDump(h types.DumpHandler) types.DumpHandler {
	return func(ctx context.Context, wg *sync.WaitGroup, resp *daemon.DumpResp, req *daemon.DumpReq) error {
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

		// Set the shell job option
		if req.GetCriu() == nil {
			req.Criu = &daemon.CriuOpts{}
		}

		req.Criu.ShellJob = isShellJob || req.GetCriu().GetShellJob()

		return h(ctx, wg, resp, req)
	}
}

// Close common file descriptors b/w the parent and child process
func CloseCommonFilesForDump(h types.DumpHandler) types.DumpHandler {
	return func(ctx context.Context, wg *sync.WaitGroup, resp *daemon.DumpResp, req *daemon.DumpReq) error {
		pid := resp.GetState().GetPID()
		if pid == 0 {
			return status.Errorf(codes.NotFound, "missing PID. Ensure an adapter sets this PID in response before.")
		}

		err := utils.CloseCommonFds(int32(os.Getpid()), int32(pid))
		if err != nil {
			return status.Errorf(codes.Internal, "failed to close common fds: %v", err)
		}

		return h(ctx, wg, resp, req)
	}
}

////////////////////////
/// Restore Adapters ///
////////////////////////

// Detect and sets network options for CRIU
// XXX YA: Enforces unsuitable options for CRIU. Some times, we may
// not want to use TCP established connections. Also, for external unix
// sockets, the flag is deprecated. The correct way is to use the
// --external flag in CRIU.
func DetectNetworkOptionsForRestore(h types.RestoreHandler) types.RestoreHandler {
	return func(ctx context.Context, wg *sync.WaitGroup, resp *daemon.RestoreResp, req *daemon.RestoreReq) error {
		var hasTCP, hasExtUnixSocket bool

		if state := resp.GetState(); state != nil {
			for _, Conn := range state.GetInfo().GetOpenConnections() {
				if Conn.Type == syscall.SOCK_STREAM { // TCP
					hasTCP = true
				}
				if Conn.Type == syscall.AF_UNIX { // Interprocess
					hasExtUnixSocket = true
				}
			}
		} else {
			log.Warn().Msg("No process info found. it should have been filled by an adapter")
		}

		// Set the network options
		if req.GetCriu() == nil {
			req.Criu = &daemon.CriuOpts{}
		}

		req.Criu.TcpEstablished = hasTCP || req.GetCriu().GetTcpEstablished()
		req.Criu.ExtUnixSk = hasExtUnixSocket || req.GetCriu().GetExtUnixSk()

		return h(ctx, wg, resp, req)
	}
}

// Detect and sets shell job option for CRIU
func DetectShellJobForRestore(h types.RestoreHandler) types.RestoreHandler {
	return func(ctx context.Context, wg *sync.WaitGroup, resp *daemon.RestoreResp, req *daemon.RestoreReq) error {
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

		// Set the shell job option
		if req.GetCriu() == nil {
			req.Criu = &daemon.CriuOpts{}
		}

		req.Criu.ShellJob = isShellJob || req.GetCriu().GetShellJob()

		return h(ctx, wg, resp, req)
	}
}

// Fill process state in the restore response
func FillProcessStateForRestore(h types.RestoreHandler) types.RestoreHandler {
	return func(ctx context.Context, wg *sync.WaitGroup, resp *daemon.RestoreResp, req *daemon.RestoreReq) error {
		// Check if path is a directory
		path := req.GetCriu().GetImagesDir()
		if path == "" {
			return status.Errorf(codes.InvalidArgument, "missing path. should have been set by an adapter")
		}

		stateFile := filepath.Join(path, STATE_FILE)
		state := &daemon.ProcessState{}

		if err := utils.LoadJSONFromFile(stateFile, state); err != nil {
			return status.Errorf(codes.Internal, "failed to load process state from dump: %v", err)
		}

		resp.State = state

		return h(ctx, wg, resp, req)
	}
}

// Open files from the dump state are inherited by the restored process.
// For e.g. the standard streams (stdin, stdout, stderr) are inherited to use
// a log file.
func InheritOpenFilesForRestore(h types.RestoreHandler) types.RestoreHandler {
	return func(ctx context.Context, wg *sync.WaitGroup, resp *daemon.RestoreResp, req *daemon.RestoreReq) error {
		extraFiles, _ := ctx.Value(types.RESTORE_EXTRA_FILES_CONTEXT_KEY).([]*os.File)
		inheritFds := req.GetCriu().GetInheritFd()
		if info := resp.GetState().GetInfo(); info != nil {
			restoreLogPath := fmt.Sprintf(RESTORE_OUTPUT_FILE_PATH_FORMATTER, time.Now().Unix())
			restoreLog, err := os.Create(restoreLogPath)
			defer restoreLog.Close()
			if err != nil {
				return status.Errorf(codes.Internal, "failed to open restore log: %v", err)
			}
			for _, f := range info.GetOpenFiles() {
				if f.Fd == 0 || f.Fd == 1 || f.Fd == 2 {
					f.Path = strings.TrimPrefix(f.Path, "/")
					extraFiles = append(extraFiles, restoreLog)
					inheritFds = append(inheritFds, &daemon.InheritFd{
						Fd:  2 + int32(len(extraFiles)),
						Key: f.Path,
					})
				} else {
					log.Warn().Msgf("found non-stdio open file %s with fd %d", f.Path, f.Fd)
				}
			}
		} else {
			log.Warn().Msg("No process info found. it should have been filled by an adapter")
		}
		ctx = context.WithValue(ctx, types.RESTORE_EXTRA_FILES_CONTEXT_KEY, extraFiles)

		// Set the inherited fds
		if req.GetCriu() == nil {
			req.Criu = &daemon.CriuOpts{}
		}
		req.GetCriu().InheritFd = inheritFds

		return h(ctx, wg, resp, req)
	}
}
