package utils

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/cedana/cedana/pkg/api/daemon"
	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/mem"
	"github.com/shirou/gopsutil/v4/process"
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
func CloseCommonFds(ctx context.Context, parentPID, childPID int32) error {
	parent, err := process.NewProcessWithContext(ctx, parentPID)
	if err != nil {
		return err
	}

	child, err := process.NewProcessWithContext(ctx, childPID)
	if err != nil {
		return err
	}

	parentFds, err := parent.OpenFilesWithContext(ctx)
	if err != nil {
		return err
	}

	childFds, err := child.OpenFilesWithContext(ctx)
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
	state.PID = pid

	p, err := process.NewProcessWithContext(ctx, int32(pid))
	if err != nil {
		return fmt.Errorf("could not get process: %v", err)
	}

	// get process uids, gids, and groups
	uids, err := p.UidsWithContext(ctx)
	if err != nil {
		return fmt.Errorf("could not get uids: %v", err)
	}
	gids, err := p.GidsWithContext(ctx)
	if err != nil {
		return fmt.Errorf("could not get gids: %v", err)
	}
	groups, err := p.GroupsWithContext(ctx)
	if err != nil {
		return fmt.Errorf("could not get groups: %v", err)
	}
	state.UIDs = uids
	state.GIDs = gids
	state.Groups = groups

	var openFiles []*daemon.OpenFilesStat
	of, err := p.OpenFilesWithContext(ctx)
	if err != nil {
		return fmt.Errorf("could not get open files: %v", err)
	}
	for _, f := range of {
		var mode string
		file, err := os.Stat(f.Path)
		if err == nil {
			mode = file.Mode().Perm().String()
		}

		openFiles = append(openFiles, &daemon.OpenFilesStat{
			Fd:   f.Fd,
			Path: f.Path,
			Mode: mode,
		})
	}

	// used for network barriers (TODO: NR)
	var openConnections []*daemon.ConnectionStat
	conns, err := p.ConnectionsWithContext(ctx)
	if err != nil {
		return fmt.Errorf("could not get connections: %v", err)
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

	memoryUsed, _ := p.MemoryPercentWithContext(ctx)
	isRunning, err := p.IsRunningWithContext(ctx)
	if err != nil {
		return fmt.Errorf("could not get process status: %v", err)
	}

	// if the process is actually running, we don't care that
	// we're potentially overriding a failed flag here.
	// In the case of a restored/resuscitated process this is a good thing

	// this is the status as returned by gopsutil.
	// ideally we want more than this, or some parsing to happen from this end
	proccessStatus, _ := p.StatusWithContext(ctx)

	// we need the cwd to ensure that it exists on the other side of the restore.
	// if it doesn't - we inheritFd it?
	cwd, _ := p.CwdWithContext(ctx)

	// system information
	cpuinfo, err := cpu.InfoWithContext(ctx)
	vcpus, err := cpu.CountsWithContext(ctx, true)
	if err == nil {
		state.CPUInfo = &daemon.CPUInfo{
			Count:      int32(vcpus),
			CPU:        cpuinfo[0].CPU,
			VendorID:   cpuinfo[0].VendorID,
			Family:     cpuinfo[0].Family,
			PhysicalID: cpuinfo[0].PhysicalID,
		}
	}

	mem, err := mem.VirtualMemoryWithContext(ctx)
	if err == nil {
		state.MemoryInfo = &daemon.MemoryInfo{
			Total:     mem.Total,
			Available: mem.Available,
			Used:      mem.Used,
		}
	}

	host, err := host.InfoWithContext(ctx)
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

// Returns a channel that will be closed when a non-child process exits
// Since, we cannot use the process.Wait() method to wait for a non-child process to exit
func WaitForPid(pid uint32) chan int {
	exitCh := make(chan int)

	go func() {
		for {
			// wait for the process to exit
			p, err := process.NewProcess(int32(pid))
			if err != nil {
				break
			}
			_, err = p.Status()
			if err != nil {
				break
			}
			time.Sleep(300 * time.Millisecond)
		}
		close(exitCh)
	}()

	return exitCh
}

func PidExists(pid uint32) bool {
	p, err := process.NewProcess(int32(pid))
	if err != nil {
		return false
	}
	_, err = p.Status()
	if err != nil {
		return false
	}
	return true
}
