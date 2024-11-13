package adapters

// Defines all the adapters that manage the process-level details

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/cedana/cedana/pkg/api/criu"
	"github.com/cedana/cedana/pkg/api/daemon"
	"github.com/cedana/cedana/pkg/types"
	"github.com/cedana/cedana/pkg/utils"
	"github.com/rs/zerolog/log"
	"github.com/shirou/gopsutil/v4/process"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

const (
	STATE_FILE                         = "process_state.json"
	DEFAULT_RESTORE_LOG_PATH_FORMATTER = "/var/log/cedana-restore-%d.log"
	RESTORE_LOG_FLAGS                  = os.O_CREATE | os.O_WRONLY | os.O_APPEND // no truncate
	RESTORE_LOG_PERMS                  = 0o644
)

////////////////////////
//// Dump Adapters /////
////////////////////////

// Check if the process exists, and is running
func CheckProcessExistsForDump(next types.Handler[types.Dump]) types.Handler[types.Dump] {
	next.Handle = func(ctx context.Context, resp *daemon.DumpResp, req *daemon.DumpReq) error {
		pid := req.GetDetails().GetPID()
		if pid == 0 {
			return status.Errorf(codes.InvalidArgument, "missing PID")
		}
		exists, err := process.PidExistsWithContext(ctx, int32(pid))
		if err != nil {
			return status.Errorf(codes.Internal, "failed to check process: %v", err)
		}
		if !exists {
			return status.Errorf(codes.NotFound, "process PID %d does not exist", pid)
		}

		if resp.GetState() == nil {
			resp.State = &daemon.ProcessState{}
		}

		resp.State.PID = uint32(pid)

		return next.Handle(ctx, resp, req)
	}
	return next
}

// Fills process state in the dump response.
// Requires at least the PID to be present in the DumpResp.State
// Also saves the state to a file in the dump directory, post dump.
func FillProcessStateForDump(next types.Handler[types.Dump]) types.Handler[types.Dump] {
	next.Handle = func(ctx context.Context, resp *daemon.DumpResp, req *daemon.DumpReq) error {
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

		err = next.Handle(ctx, resp, req)
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
	return next
}

// Detect and sets network options for CRIU
// XXX YA: Enforces unsuitable options for CRIU. Some times, we may
// not want to use TCP established connections. Also, for external unix
// sockets, the flag is deprecated. The correct way is to use the
// --external flag in CRIU.
func DetectNetworkOptionsForDump(next types.Handler[types.Dump]) types.Handler[types.Dump] {
	next.Handle = func(ctx context.Context, resp *daemon.DumpResp, req *daemon.DumpReq) error {
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

		if req.GetCriu() == nil {
			req.Criu = &criu.CriuOpts{}
		}

		// Only set unless already set
		if req.GetCriu().TcpEstablished == nil {
			req.Criu.TcpEstablished = proto.Bool(hasTCP)
		}
		if req.GetCriu().ExtUnixSk == nil {
			req.Criu.ExtUnixSk = proto.Bool(hasExtUnixSocket)
		}

		return next.Handle(ctx, resp, req)
	}
	return next
}

// Detect and sets shell job option for CRIU
func DetectShellJobForDump(next types.Handler[types.Dump]) types.Handler[types.Dump] {
	next.Handle = func(ctx context.Context, resp *daemon.DumpResp, req *daemon.DumpReq) error {
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
			req.Criu = &criu.CriuOpts{}
		}

		// Only set unless already set
		if req.GetCriu().ShellJob == nil {
			req.Criu.ShellJob = proto.Bool(isShellJob)
		}

		return next.Handle(ctx, resp, req)
	}
	return next
}

// Close common file descriptors b/w the parent and child process
func CloseCommonFilesForDump(next types.Handler[types.Dump]) types.Handler[types.Dump] {
	next.Handle = func(ctx context.Context, resp *daemon.DumpResp, req *daemon.DumpReq) error {
		pid := resp.GetState().GetPID()
		if pid == 0 {
			return status.Errorf(codes.NotFound, "missing PID. Ensure an adapter sets this PID in response before.")
		}

		err := utils.CloseCommonFds(ctx, int32(os.Getpid()), int32(pid))
		if err != nil {
			return status.Errorf(codes.Internal, "failed to close common fds: %v", err)
		}

		return next.Handle(ctx, resp, req)
	}
	return next
}

////////////////////////
/// Restore Adapters ///
////////////////////////

// Fill process state in the restore response
func FillProcessStateForRestore(next types.Handler[types.Restore]) types.Handler[types.Restore] {
	next.Handle = func(ctx context.Context, resp *daemon.RestoreResp, req *daemon.RestoreReq) (exited chan int, err error) {
		// Check if path is a directory
		path := req.GetCriu().GetImagesDir()
		if path == "" {
			return nil, status.Errorf(codes.InvalidArgument, "missing path. should have been set by an adapter")
		}

		stateFile := filepath.Join(path, STATE_FILE)
		state := &daemon.ProcessState{}

		if err := utils.LoadJSONFromFile(stateFile, state); err != nil {
			return nil, status.Errorf(codes.Internal, "failed to load process state from dump: %v", err)
		}

		resp.State = state

		exited, err = next.Handle(ctx, resp, req)
		if err != nil {
			return exited, err
		}

		// Try to update the process state with the latest information,
		// Only possible if process is still running, otherwise ignore.
		_ = utils.FillProcessState(ctx, state.PID, state)

		return exited, err
	}
	return next
}

// Detect and sets network options for CRIU
// XXX YA: Enforces unsuitable options for CRIU. Some times, we may
// not want to use TCP established connections. Also, for external unix
// sockets, the flag is deprecated. The correct way is to use the
// --external flag in CRIU.
func DetectNetworkOptionsForRestore(next types.Handler[types.Restore]) types.Handler[types.Restore] {
	next.Handle = func(ctx context.Context, resp *daemon.RestoreResp, req *daemon.RestoreReq) (chan int, error) {
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

		if req.GetCriu() == nil {
			req.Criu = &criu.CriuOpts{}
		}

		// Only set unless already set
		if req.GetCriu().TcpEstablished == nil {
			req.Criu.TcpEstablished = proto.Bool(hasTCP)
		}
		if req.GetCriu().ExtUnixSk == nil {
			req.Criu.ExtUnixSk = proto.Bool(hasExtUnixSocket)
		}

		return next.Handle(ctx, resp, req)
	}
	return next
}

// Detect and sets shell job option for CRIU
func DetectShellJobForRestore(next types.Handler[types.Restore]) types.Handler[types.Restore] {
	next.Handle = func(ctx context.Context, resp *daemon.RestoreResp, req *daemon.RestoreReq) (chan int, error) {
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
			req.Criu = &criu.CriuOpts{}
		}

		// Only set unless already set
		if req.GetCriu().ShellJob == nil {
			req.Criu.ShellJob = proto.Bool(isShellJob)
		}

		return next.Handle(ctx, resp, req)
	}
	return next
}

// Open files from the dump state are inherited by the restored process.
// For e.g. the standard streams (stdin, stdout, stderr) are inherited to use
// a log file.
func InheritOpenFilesForRestore(next types.Handler[types.Restore]) types.Handler[types.Restore] {
	next.Handle = func(ctx context.Context, resp *daemon.RestoreResp, req *daemon.RestoreReq) (exited chan int, err error) {
		extraFiles, _ := ctx.Value(types.RESTORE_EXTRA_FILES_CONTEXT_KEY).([]*os.File)
		ioFiles, _ := ctx.Value(types.RESTORE_IO_FILES_CONTEXT_KEY).([]*os.File)
		inheritFds := req.GetCriu().GetInheritFd()

		if info := resp.GetState().GetInfo(); info != nil {
			// In case of attach, we need to create pipes for stdin, stdout, stderr
			if req.Attach {
				inReader, inWriter, err := os.Pipe()
				outReader, outWriter, err := os.Pipe()
				errReader, errWriter, err := os.Pipe()
				if err != nil {
					return nil, status.Errorf(codes.Internal, "failed to create pipes for attach: %v", err)
				}
				ioFiles = append(ioFiles, inWriter, outReader, errReader)
				for _, f := range info.GetOpenFiles() {
					f.Path = strings.TrimPrefix(f.Path, "/")
					if f.Fd == 0 {
						extraFiles = append(extraFiles, inReader)
						inheritFds = append(inheritFds, &criu.InheritFd{
							Fd:  proto.Int32(2 + int32(len(extraFiles))),
							Key: proto.String(f.Path),
						})
						defer inReader.Close()
					} else if f.Fd == 1 {
						extraFiles = append(extraFiles, outWriter)
						inheritFds = append(inheritFds, &criu.InheritFd{
							Fd:  proto.Int32(2 + int32(len(extraFiles))),
							Key: proto.String(f.Path),
						})
						defer outWriter.Close()
					} else if f.Fd == 2 {
						extraFiles = append(extraFiles, errWriter)
						inheritFds = append(inheritFds, &criu.InheritFd{
							Fd:  proto.Int32(2 + int32(len(extraFiles))),
							Key: proto.String(f.Path),
						})
						defer errWriter.Close()
					}
				}

				// In case of log, we need to open a log file
			} else {
				if req.Log == "" {
					req.Log = fmt.Sprintf(DEFAULT_RESTORE_LOG_PATH_FORMATTER, time.Now().Unix())
				}
				restoreLog, err := os.OpenFile(req.Log, RESTORE_LOG_FLAGS, RESTORE_LOG_PERMS)
				defer restoreLog.Close()
				if err != nil {
					return nil, status.Errorf(codes.Internal, "failed to open restore log: %v", err)
				}
				for _, f := range info.GetOpenFiles() {
					if f.Fd == 1 || f.Fd == 2 {
						f.Path = strings.TrimPrefix(f.Path, "/")
						extraFiles = append(extraFiles, restoreLog)
						inheritFds = append(inheritFds, &criu.InheritFd{
							Fd:  proto.Int32(2 + int32(len(extraFiles))),
							Key: proto.String(f.Path),
						})
					} else {
						log.Warn().Msgf("found non-stdio open file %s with fd %d", f.Path, f.Fd)
					}
				}
			}
		} else {
			log.Warn().Msg("No process info found. it should have been filled by an adapter")
		}

		ctx = context.WithValue(ctx, types.RESTORE_EXTRA_FILES_CONTEXT_KEY, extraFiles)
		ctx = context.WithValue(ctx, types.RESTORE_IO_FILES_CONTEXT_KEY, ioFiles)

		// Set the inherited fds
		if req.GetCriu() == nil {
			req.Criu = &criu.CriuOpts{}
		}
		req.GetCriu().InheritFd = inheritFds

		return next.Handle(ctx, resp, req)
	}
	return next
}
