package utils

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/cedana/cedana/pkg/api/daemon"
	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/mem"
	"github.com/shirou/gopsutil/v4/process"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Checks if the given process has any active tcp connections
func HasActiveTCPConnections(pid int32) (bool, error) {
	tcpFile := filepath.Join("/proc", fmt.Sprintf("%d", pid), "net/tcp")

	file, err := os.Open(tcpFile)
	if err != nil {
		return false, fmt.Errorf("failed to open %s: %v", tcpFile, err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "  sl") {
			continue
		}
		return true, nil
	}

	if err := scanner.Err(); err != nil {
		return false, fmt.Errorf("error reading %s: %v", tcpFile, err)
	}

	return false, nil
}

// CloseCommonFdscloses any common FDs between the parent and child process
func CloseCommonFds(parentPID, childPID int32) error {
	parent, err := process.NewProcess(parentPID)
	if err != nil {
		return err
	}

	child, err := process.NewProcess(childPID)
	if err != nil {
		return err
	}

	parentFds, err := parent.OpenFiles()
	if err != nil {
		return err
	}

	childFds, err := child.OpenFiles()
	if err != nil {
		return err
	}

	for _, pfd := range parentFds {
		for _, cfd := range childFds {
			if pfd.Path == cfd.Path && strings.Contains(pfd.Path, ".pid") {
				// we have a match, close the FD
				err := syscall.Close(int(cfd.Fd))
				if err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func FillProcessState(ctx context.Context, pid uint32, state *daemon.ProcessState) error {
	if state == nil {
		return fmt.Errorf("state is nil")
	}

	p, err := process.NewProcessWithContext(ctx, int32(pid))
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
	cwd, _ := p.Cwd()

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

	return nil
}
