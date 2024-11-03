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

	"github.com/cedana/cedana/pkg/api/daemon"
	"github.com/cedana/cedana/pkg/types"
	"github.com/cedana/cedana/pkg/utils"
	"github.com/rs/zerolog/log"
	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/mem"
	"github.com/shirou/gopsutil/v4/process"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	STATE_FILE                        = "process_state.json"
	RESTORE_OUTPUT_LOG_PATH_FORMATTER = "/var/log/cedana-restore-%d.log"
)

////////////////////////
//// Dump Adapters /////
////////////////////////

// Check if the process exists, and is running
func CheckProcessExistsForDump(h types.DumpHandler) types.DumpHandler {
	return func(ctx context.Context, resp *daemon.DumpResp, req *daemon.DumpReq) error {
		pid := req.GetDetails().GetPID()
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

		return h(ctx, resp, req)
	}
}

// Fills process state in the dump response.
// Requires at least the PID to be present in the DumpResp.State
// Also saves the state to a file in the dump directory, post dump.
func FillProcessStateForDump(h types.DumpHandler) types.DumpHandler {
	return func(ctx context.Context, resp *daemon.DumpResp, req *daemon.DumpReq) error {
		state := resp.GetState()
		if state == nil {
			return status.Errorf(codes.InvalidArgument, "missing state. at least PID is required in resp.state")
		}

		if state.PID == 0 {
			return status.Errorf(codes.NotFound, "missing PID. Ensure an adapter sets this PID in response.")
		}

		p, err := process.NewProcessWithContext(ctx, int32(state.PID))
		if err != nil {
			return status.Errorf(codes.NotFound, "process not found: %v", err)
		}

		// get process uids, gids, and groups
		uids, err := p.UidsWithContext(ctx)
		if err != nil {
			return status.Errorf(codes.Internal, "could not get uids: %v", err)
		}
		gids, err := p.GidsWithContext(ctx)
		if err != nil {
			return status.Errorf(codes.Internal, "could not get gids: %v", err)
		}
		groups, err := p.GroupsWithContext(ctx)
		if err != nil {
			return status.Errorf(codes.Internal, "could not get groups: %v", err)
		}
		state.UIDs = uids
		state.GIDs = gids
		state.Groups = groups

		var openFiles []*daemon.OpenFilesStat
		of, err := p.OpenFiles()
		if err != nil {
			return status.Errorf(codes.Internal, "could not get open files: %v", err)
		}
		for _, f := range of {
			file, err := os.Stat(f.Path)
			if err != nil {
				continue
			}
			mode := file.Mode().Perm().String()

			openFiles = append(openFiles, &daemon.OpenFilesStat{
				Fd:   f.Fd,
				Path: f.Path,
				Mode: mode,
			})
		}

		// used for network barriers (TODO: NR)
		var openConnections []*daemon.ConnectionStat
		conns, err := p.Connections()
		if err != nil {
			return status.Errorf(codes.Internal, "could not get connections: %v", err)
		}
		for _, conn := range conns {
			Laddr := &daemon.Addr{
				IP:   conn.Laddr.IP,
				Port: conn.Laddr.Port,
			}
			Raddr := &daemon.Addr{
				IP:   conn.Raddr.IP,
				Port: conn.Raddr.Port,
			}
			openConnections = append(openConnections, &daemon.ConnectionStat{
				Fd:     conn.Fd,
				Family: conn.Family,
				Type:   conn.Type,
				Laddr:  Laddr,
				Raddr:  Raddr,
				Status: conn.Status,
				PID:    uint32(conn.Pid),
				UIDs:   conn.Uids,
			})
		}

		memoryUsed, _ := p.MemoryPercent()
		isRunning, err := p.IsRunning()
		if err != nil {
			return status.Errorf(codes.Internal, "could not check if process is running: %v", err)
		}

		// if the process is actually running, we don't care that
		// we're potentially overriding a failed flag here.
		// In the case of a restored/resuscitated process this is a good thing

		// this is the status as returned by gopsutil.
		// ideally we want more than this, or some parsing to happen from this end
		proccessStatus, _ := p.Status()

		// we need the cwd to ensure that it exists on the other side of the restore.
		// if it doesn't - we inheritFd it?
		cwd, err := p.Cwd()
		if err != nil {
			return status.Errorf(codes.Internal, "could not get cwd: %v", err)
		}

		// system information
		cpuinfo, err := cpu.Info()
		vcpus, err := cpu.Counts(true)
		if err == nil {
			state.CPUInfo = &daemon.CPUInfo{
				Count:      int32(vcpus),
				CPU:        cpuinfo[0].CPU,
				VendorID:   cpuinfo[0].VendorID,
				Family:     cpuinfo[0].Family,
				PhysicalID: cpuinfo[0].PhysicalID,
			}
		}

		mem, err := mem.VirtualMemory()
		if err == nil {
			state.MemoryInfo = &daemon.MemoryInfo{
				Total:     mem.Total,
				Available: mem.Available,
				Used:      mem.Used,
			}
		}

		host, err := host.Info()
		if err == nil {
			state.HostInfo = &daemon.HostInfo{
				HostID:               host.HostID,
				Hostname:             host.Hostname,
				OS:                   host.OS,
				Platform:             host.Platform,
				KernelVersion:        host.KernelVersion,
				KernelArch:           host.KernelArch,
				VirtualizationSystem: host.VirtualizationSystem,
				VirtualizationRole:   host.VirtualizationRole,
			}
		}

		// ignore sending network for now, little complicated
		state.Info = &daemon.ProcessInfo{
			OpenFiles:       openFiles,
			WorkingDir:      cwd,
			MemoryPercent:   memoryUsed,
			IsRunning:       isRunning,
			OpenConnections: openConnections,
			Status:          strings.Join(proccessStatus, ""),
		}

		err = h(ctx, resp, req)
		if err != nil {
			return err
		}

		// Post dump, save the state to a file in the dump
		stateFile := filepath.Join(req.GetDetails().GetCriu().GetImagesDir(), STATE_FILE)
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
	return func(ctx context.Context, resp *daemon.DumpResp, req *daemon.DumpReq) error {
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
		if req.GetDetails().GetCriu() == nil {
			req.Details.Criu = &daemon.CriuOpts{}
		}

		req.Details.Criu.TcpEstablished = hasTCP || req.GetDetails().GetCriu().GetTcpEstablished()
		req.Details.Criu.ExtUnixSk = hasExtUnixSocket || req.GetDetails().GetCriu().GetExtUnixSk()

		return h(ctx, resp, req)
	}
}

// Detect and sets shell job option for CRIU
func DetectShellJobForDump(h types.DumpHandler) types.DumpHandler {
	return func(ctx context.Context, resp *daemon.DumpResp, req *daemon.DumpReq) error {
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
		if req.GetDetails().GetCriu() == nil {
			req.Details.Criu = &daemon.CriuOpts{}
		}

		req.Details.Criu.ShellJob = isShellJob || req.GetDetails().GetCriu().GetShellJob()

		return h(ctx, resp, req)
	}
}

// Close common file descriptors b/w the parent and child process
func CloseCommonFilesForDump(h types.DumpHandler) types.DumpHandler {
	return func(ctx context.Context, resp *daemon.DumpResp, req *daemon.DumpReq) error {
		pid := resp.GetState().GetPID()
		if pid == 0 {
			return status.Errorf(codes.NotFound, "missing PID. Ensure an adapter sets this PID in response before.")
		}

		err := utils.CloseCommonFds(int32(os.Getpid()), int32(pid))
		if err != nil {
			return status.Errorf(codes.Internal, "failed to close common fds: %v", err)
		}

		return h(ctx, resp, req)
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
	return func(ctx context.Context, resp *daemon.RestoreResp, req *daemon.RestoreReq) error {
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
		if req.GetDetails().GetCriu() == nil {
			req.Details.Criu = &daemon.CriuOpts{}
		}

		req.Details.Criu.TcpEstablished = hasTCP || req.GetDetails().GetCriu().GetTcpEstablished()
		req.Details.Criu.ExtUnixSk = hasExtUnixSocket || req.GetDetails().GetCriu().GetExtUnixSk()

		return h(ctx, resp, req)
	}
}

// Detect and sets shell job option for CRIU
func DetectShellJobForRestore(h types.RestoreHandler) types.RestoreHandler {
	return func(ctx context.Context, resp *daemon.RestoreResp, req *daemon.RestoreReq) error {
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
		if req.GetDetails().GetCriu() == nil {
			req.Details.Criu = &daemon.CriuOpts{}
		}

		req.Details.Criu.ShellJob = isShellJob || req.GetDetails().GetCriu().GetShellJob()

		return h(ctx, resp, req)
	}
}

// Fill process state in the restore response
func FillProcessStateForRestore(h types.RestoreHandler) types.RestoreHandler {
	return func(ctx context.Context, resp *daemon.RestoreResp, req *daemon.RestoreReq) error {
		// Check if path is a directory
		path := req.GetDetails().GetCriu().GetImagesDir()
		if path == "" {
			return status.Errorf(codes.InvalidArgument, "missing path. should have been set by an adapter")
		}

		stateFile := filepath.Join(path, STATE_FILE)
		state := &daemon.ProcessState{}

		if err := utils.LoadJSONFromFile(stateFile, state); err != nil {
			return status.Errorf(codes.Internal, "failed to load process state from dump: %v", err)
		}

		resp.State = state

		return h(ctx, resp, req)
	}
}

// Open files from the dump state are inherited by the restored process.
// For e.g. the standard streams (stdin, stdout, stderr) are inherited to use
// a log file.
func InheritOpenFilesForRestore(h types.RestoreHandler) types.RestoreHandler {
	return func(ctx context.Context, resp *daemon.RestoreResp, req *daemon.RestoreReq) error {
		extraFiles, _ := ctx.Value(types.RESTORE_EXTRA_FILES_CONTEXT_KEY).([]*os.File)
		inheritFds := req.GetDetails().GetCriu().GetInheritFd()
		if info := resp.GetState().GetInfo(); info != nil {
			restoreLogPath := fmt.Sprintf(RESTORE_OUTPUT_LOG_PATH_FORMATTER, time.Now().Unix())
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
		if req.GetDetails().GetCriu() == nil {
			req.GetDetails().Criu = &daemon.CriuOpts{}
		}
		req.GetDetails().GetCriu().InheritFd = inheritFds

		return h(ctx, resp, req)
	}
}
