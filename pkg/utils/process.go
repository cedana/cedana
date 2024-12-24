package utils

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"syscall"
	"time"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/mattn/go-isatty"
	"github.com/moby/sys/mountinfo"
	"github.com/shirou/gopsutil/v4/process"
)

type FdInfo struct {
	Pos   int32
	Flags int32
	MntId int32
	Inode int32
}

func GetProcessState(ctx context.Context, pid uint32) (*daemon.ProcessState, error) {
	state := &daemon.ProcessState{}
	err := FillProcessState(ctx, pid, state)
	return state, err
}

func FillProcessState(ctx context.Context, pid uint32, state *daemon.ProcessState) error {
	if state == nil {
		return fmt.Errorf("state is nil")
	}
	state.PID = pid

	errs := []error{}

	p, err := process.NewProcessWithContext(ctx, int32(pid))
	if err != nil {
		return fmt.Errorf("could not get process: %v", err)
	}

	name, _ := p.NameWithContext(ctx)
	args, _ := p.CmdlineSliceWithContext(ctx)
	if name != "" {
		state.Task = fmt.Sprintf("%s %s", name, strings.Join(args, " "))
	}

	startTime, err := p.CreateTime()
	errs = append(errs, err)
	if err == nil {
		state.StartTime = uint64(startTime)
	}

	sid, err := GetSID(pid)
	if err == nil {
		state.SID = sid
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

	var openFiles []*daemon.File
	of, err := p.OpenFilesWithContext(ctx)
	if err != nil {
		return fmt.Errorf("could not get open files: %v", err)
	}
	for _, f := range of {
		var mode string
		file, err := os.Stat(f.Path)
		if err == nil {
			mode = file.Mode().String()
			fdInfo, err := GetFdInfo(pid, int(f.Fd))
			if err != nil {
				return fmt.Errorf("could not get fd info: %v", err)
			}
			isTTY, err := IsTTY(f.Path)
			if err != nil {
				return fmt.Errorf("could not get tty info: %v", err)
			}

			openFiles = append(openFiles, &daemon.File{
				Fd:      f.Fd,
				Path:    f.Path,
				Mode:    mode,
				MountID: fdInfo.MntId,
				Inode:   fdInfo.Inode,
				IsTTY:   isTTY,
			})
		}
	}

	mountinfoFile, err := os.Open(fmt.Sprintf("/proc/%d/mountinfo", pid))
	if err != nil {
		return fmt.Errorf("could not open mountinfo: %v", err)
	}
	mounts, err := mountinfo.GetMountsFromReader(mountinfoFile, nil)
	if err != nil {
		return fmt.Errorf("could not get mounts: %v", err)
	}
	for _, m := range mounts {
		state.Mounts = append(state.Mounts, &daemon.Mount{
			ID:         int32(m.ID),
			Parent:     int32(m.Parent),
			Major:      int32(m.Major),
			Minor:      int32(m.Minor),
			Root:       m.Root,
			MountPoint: m.Mountpoint,
			Options:    m.Options,
			FSType:     m.FSType,
		})
	}

	// used for network barriers (TODO: NR)
	var openConnections []*daemon.Connection
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
		openConnections = append(openConnections, &daemon.Connection{
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

	memoryUsed, err := p.MemoryPercentWithContext(ctx)
	errs = append(errs, err)
	proccessStatus, err := p.StatusWithContext(ctx)
	errs = append(errs, err)
	cwd, err := p.CwdWithContext(ctx)
	errs = append(errs, err)

	state.MemoryPercent = memoryUsed
	state.OpenFiles = openFiles
	state.OpenConnections = openConnections
	state.WorkingDir = cwd
	state.Status = strings.Join(proccessStatus, "")

	state.Host, err = GetHost(ctx)
	errs = append(errs, err)

	return errors.Join(errs...)
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

// Returns a channel that will be closed when a non-child process exits
// Since, we cannot use the process.Wait() method to wait for a non-child process to exit
// It waits until the process is a zombie process
func WaitForPid(pid uint32) chan int {
	exitCh := make(chan int)

	go func() {
		defer close(exitCh)
		for {
			// wait for the process to exit
			p, err := process.NewProcess(int32(pid))
			if err != nil {
				return
			}
			s, err := p.Status()
			if err != nil {
				return
			}
			for _, status := range s {
				if status == "zombie" {
					return
				}
			}
			time.Sleep(300 * time.Millisecond)
		}
	}()

	return exitCh
}

// Returns a channel that will be closed when a non-child process exits.
// Since, we cannot use the process.Wait() method to wait for a non-child process to exit.
// It waits until the process is a zombie process, or the process is not found.
func WaitForPidCtx(ctx context.Context, pid uint32) chan int {
	exitCh := make(chan int)

	go func() {
		defer close(exitCh)
		for {
			if ctx.Err() != nil {
				return
			}
			// wait for the process to exit
			p, err := process.NewProcessWithContext(ctx, int32(pid))
			if err != nil {
				return
			}
			s, err := p.Status()
			if err != nil {
				return
			}
			for _, status := range s {
				if status == "zombie" {
					return
				}
			}
			time.Sleep(300 * time.Millisecond)
		}
	}()

	return exitCh
}

// PidExists checks if a process with the given PID exists
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

// FdInfo returns file descriptor information for the provided process and file descriptor.
func GetFdInfo(pid uint32, fd int) (*FdInfo, error) {
	path := fmt.Sprintf("/proc/%d/fdinfo/%d", pid, fd)
	contents, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("Could not read fdinfo file: %s", err)
	}

	// Parse the fdinfo file
	var info FdInfo
	lines := strings.Split(string(contents), "\n")
	for _, line := range lines {
		parts := strings.Split(line, ":")
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		switch key {
		case "pos":
			fmt.Sscanf(value, "%d", &info.Pos)
		case "flags":
			fmt.Sscanf(value, "%d", &info.Flags)
		case "mnt_id":
			fmt.Sscanf(value, "%d", &info.MntId)
		case "ino":
			fmt.Sscanf(value, "%d", &info.Inode)
		}
	}

	return &info, nil
}

func GetSID(pid uint32) (uint32, error) {
	sid, _, errno := syscall.Syscall(syscall.SYS_GETSID, uintptr(pid), 0, 0)
	if errno != 0 {
		return 0, fmt.Errorf("errno: %v", errno)
	}
	return uint32(sid), nil
}

func GetCmd(ctx context.Context, pid uint32) (string, []string, error) {
	p, err := process.NewProcess(int32(pid))
	if err != nil {
		return "", nil, err
	}
	name, err := p.NameWithContext(ctx)
	if err != nil {
		return "", nil, err
	}
	args, err := p.CmdlineSliceWithContext(ctx)
	if err != nil {
		return "", nil, err
	}
	return name, args, nil
}

func IsTTY(path string) (bool, error) {
	file, err := os.Open(path)
	if err != nil {
		return false, err
	}
	return isatty.IsTerminal(file.Fd()), nil
}
